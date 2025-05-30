package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/klauspost/compress/snappy"
	"github.com/prometheus/prometheus/prompb"
	_ "github.com/lib/pq"
)

type Metric struct {
	Name      string
	OrgID     string
	Labels    map[string]string
	Timestamp int64
	Value     float64
}

type DailySystemCPU struct {
	ID           int       `json:"id"`
	SystemID     string    `json:"system_id"`
	DisplayName  string    `json:"display_name"`
	OrgID        string    `json:"org_id"`
	Product      string    `json:"product"`
	SocketCount  int       `json:"socket_count"`
	Date         time.Time `json:"date"`
	TotalUptime  float64   `json:"total_uptime"`
}

type MeteringResponse struct {
	Metadata struct {
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	} `json:"metadata"`
	Data []DailySystemCPU `json:"data"`
}

func main() {
	// Database connection parameters
	dbHost := os.Getenv("POSTGRES_HOST")
	dbPort := os.Getenv("POSTGRES_PORT")
	dbUser := os.Getenv("POSTGRES_USER")
	dbPassword := os.Getenv("POSTGRES_PASSWORD")
	dbName := os.Getenv("POSTGRES_DB")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Verify database connection
	err = db.Ping()
	if err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	// Set up HTTP server
	server := &http.Server{
		Addr: ":" + getPort(),
	}

	// Register handlers
	http.HandleFunc("/receive", handlePrometheusWrite(db))
	http.HandleFunc("/health", handleHealth(db))
	http.HandleFunc("/api/metering/v1/system_cpu_logical_count", handleMeteringQuery(db))

	log.Printf("Starting server on port %s", getPort())
	err = server.ListenAndServe()
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func getPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return port
}

func handleHealth(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check database connectivity
		if err := db.Ping(); err != nil {
			http.Error(w, fmt.Sprintf("Database ping failed: %v", err), http.StatusInternalServerError)
			return
		}

		// Return health status
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func handlePrometheusWrite(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read and decompress the request body
		compressed, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		body, err := snappy.Decode(nil, compressed)
		if err != nil {
			http.Error(w, "Failed to decompress body", http.StatusBadRequest)
			return
		}

		// Parse Prometheus WriteRequest
		var req prompb.WriteRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			http.Error(w, "Failed to parse WriteRequest", http.StatusBadRequest)
			return
		}

		// Process and store metrics
		for _, ts := range req.Timeseries {
			metric := Metric{
				Labels:    make(map[string]string),
				Timestamp: 0,
				Value:     0,
			}

			// Extract metric name and labels
			for _, label := range ts.Labels {
				if label.Name == "__name__" {
					metric.Name = label.Value
				} else {
					metric.Labels[label.Name] = label.Value
				}
			}

			// Extract org_id from external_organization label
			orgID, ok := metric.Labels["external_organization"]
			if !ok || orgID == "" {
				log.Printf("Missing or empty external_organization label in metric: %s", metric.Name)
				http.Error(w, "Missing external_organization label", http.StatusBadRequest)
				return
			}
			metric.OrgID = orgID

			// Extract timestamp and value from samples
			for _, sample := range ts.Samples {
				metric.Timestamp = sample.Timestamp
				metric.Value = sample.Value
			}

			// Convert labels to JSON for PostgreSQL JSONB
			labelsJSON, err := json.Marshal(metric.Labels)
			if err != nil {
				log.Printf("Failed to marshal labels for metric %s: %v", metric.Name, err)
				continue
			}

			// Insert metric into database
			_, err = db.Exec(`
				INSERT INTO metrics (name, org_id, labels, timestamp, value)
				VALUES ($1, $2, $3, $4, $5)
			`, metric.Name, metric.OrgID, labelsJSON, metric.Timestamp, metric.Value)
			if err != nil {
				log.Printf("Failed to insert metric %s: %v", metric.Name, err)
				continue
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

func handleMeteringQuery(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse query parameters
		params := r.URL.Query()

		// SystemID (exact match)
		systemID := params.Get("system_id")
		if systemID != "" {
			// Validate UUID
			if _, err := parseUUID(systemID); err != nil {
				http.Error(w, "Invalid system_id format", http.StatusBadRequest)
				return
			}
		}

		// OrgID (exact match)
		orgID := params.Get("org_id")

		// DisplayName (ILIKE match)
		displayName := params.Get("display_name")
		if displayName != "" {
			displayName = "%" + strings.ReplaceAll(displayName, "%", "\\%") + "%"
		}

		// StartDate (default: beginning of current month)
		now := time.Now()
		startDate := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		if sd := params.Get("start_date"); sd != "" {
			parsed, err := time.Parse("2006-01-02", sd)
			if err != nil {
				http.Error(w, "Invalid start_date format, use YYYY-MM-DD", http.StatusBadRequest)
				return
			}
			startDate = parsed
		}

		// EndDate (default: current day)
		endDate := now.Truncate(24 * time.Hour)
		if ed := params.Get("end_date"); ed != "" {
			parsed, err := time.Parse("2006-01-02", ed)
			if err != nil {
				http.Error(w, "Invalid end_date format, use YYYY-MM-DD", http.StatusBadRequest)
				return
			}
			endDate = parsed
		}

		// Validate date range
		if endDate.Before(startDate) {
			http.Error(w, "end_date cannot be before start_date", http.StatusBadRequest)
			return
		}

		// Limit (default: 100, max: 10000)
		limit := 100
		if l := params.Get("limit"); l != "" {
			parsed, err := strconv.Atoi(l)
			if err != nil || parsed < 1 {
				http.Error(w, "Invalid limit, must be a positive integer", http.StatusBadRequest)
				return
			}
			if parsed > 10000 {
				parsed = 10000
			}
			limit = parsed
		}

		// Offset (default: 0)
		offset := 0
		if o := params.Get("offset"); o != "" {
			parsed, err := strconv.Atoi(o)
			if err != nil || parsed < 0 {
				http.Error(w, "Invalid offset, must be a non-negative integer", http.StatusBadRequest)
				return
			}
			offset = parsed
		}

		// Build query
		conditions := []string{"date >= $1 AND date <= $2"}
		args := []interface{}{startDate, endDate}
		argIndex := 3

		if systemID != "" {
			conditions = append(conditions, fmt.Sprintf("system_id = $%d", argIndex))
			args = append(args, systemID)
			argIndex++
		}
		if orgID != "" {
			conditions = append(conditions, fmt.Sprintf("org_id = $%d", argIndex))
			args = append(args, orgID)
			argIndex++
		}
		if displayName != "" {
			conditions = append(conditions, fmt.Sprintf("display_name ILIKE $%d", argIndex))
			args = append(args, displayName)
			argIndex++
		}

		whereClause := "WHERE " + strings.Join(conditions, " AND ")
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM daily_system_cpu_logical_count %s", whereClause)
		selectQuery := fmt.Sprintf(`
			SELECT id, system_id, display_name, org_id, product, socket_count, date, total_uptime
			FROM daily_system_cpu_logical_count
			%s
			ORDER BY date DESC, system_id
			LIMIT $%d OFFSET $%d
		`, whereClause, argIndex, argIndex+1)
		args = append(args, limit, offset)

		// Get total count
		var total int
		err := db.QueryRow(countQuery, args[:argIndex-2]...).Scan(&total)
		if err != nil {
			log.Printf("Failed to count records: %v", err)
			http.Error(w, "Database query failed", http.StatusInternalServerError)
			return
		}

		// Query data
		rows, err := db.Query(selectQuery, args...)
		if err != nil {
			log.Printf("Failed to query records: %v", err)
			http.Error(w, "Database query failed", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var results []DailySystemCPU
		for rows.Next() {
			var record DailySystemCPU
			var date time.Time
			err := rows.Scan(&record.ID, &record.SystemID, &record.DisplayName, &record.OrgID,
				&record.Product, &record.SocketCount, &date, &record.TotalUptime)
			if err != nil {
				log.Printf("Failed to scan record: %v", err)
				continue
			}
			record.Date = date
			results = append(results, record)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Error iterating rows: %v", err)
			http.Error(w, "Database query failed", http.StatusInternalServerError)
			return
		}

		// Handle response format
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "text/csv") {
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Content-Disposition", "attachment; filename=system_cpu_logical_count.csv")
			writer := csv.NewWriter(w)
			defer writer.Flush()

			// Write CSV headers
			err := writer.Write([]string{
				"id", "system_id", "display_name", "org_id", "product", "socket_count", "date", "total_uptime",
			})
			if err != nil {
				log.Printf("Failed to write CSV headers: %v", err)
				http.Error(w, "Failed to generate CSV", http.StatusInternalServerError)
				return
			}

			// Write CSV rows
			for _, record := range results {
				err := writer.Write([]string{
					strconv.Itoa(record.ID),
					record.SystemID,
					record.DisplayName,
					record.OrgID,
					record.Product,
					strconv.Itoa(record.SocketCount),
					record.Date.Format("2006-01-02"),
					fmt.Sprintf("%.4f", record.TotalUptime),
				})
				if err != nil {
					log.Printf("Failed to write CSV row: %v", err)
					continue
				}
			}
			return
		}

		// Default to JSON
		response := MeteringResponse{}
		response.Metadata.Total = total
		response.Metadata.Limit = limit
		response.Metadata.Offset = offset
		response.Data = results

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		data, err := json.Marshal(response)
		if err != nil {
			log.Printf("Failed to encode JSON: %v", err)
			http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
			return
		}
		_, err = w.Write(data)
		if err != nil {
			log.Printf("Failed to write JSON: %v", err)
			return
		}
	}
}

func parseUUID(s string) (string, error) {
	if len(s) != 36 {
		return "", fmt.Errorf("invalid UUID length")
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return "", fmt.Errorf("invalid UUID format at position %d", i)
			}
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return "", fmt.Errorf("invalid UUID character at position %d", i)
		}
	}
	return s, nil
}
