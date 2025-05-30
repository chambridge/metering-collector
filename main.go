package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/klauspost/compress/snappy"
	"github.com/prometheus/prometheus/prompb"
	_ "github.com/lib/pq"
	"io"
	"log"
	"net/http"
	"os"
)

type Metric struct {
	Name      string
	OrgID     string
	Labels    map[string]string
	Timestamp int64
	Value     float64
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
