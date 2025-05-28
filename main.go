package main

import (
	"crypto/tls"
	"crypto/x509"
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

	// Configure TLS if certificate and key paths are provided
	caCertPath := os.Getenv("CA_CERT_PATH")
	serverCertPath := os.Getenv("SERVER_CERT_PATH")
	serverKeyPath := os.Getenv("SERVER_KEY_PATH")
	useTLS := caCertPath != "" && serverCertPath != "" && serverKeyPath != ""

	if useTLS {
		caCert, err := os.ReadFile(caCertPath)
		if err != nil {
			log.Fatalf("Failed to read CA certificate: %v", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			log.Fatalf("Failed to append CA certificate to pool")
		}

		server.TLSConfig = &tls.Config{
			ClientCAs:  caCertPool,
			ClientAuth: tls.RequireAndVerifyClientCert,
		}
	}

	http.HandleFunc("/receive", handlePrometheusWrite(db))
	log.Printf("Starting server on port %s (TLS: %v)", getPort(), useTLS)
	if useTLS {
		err = server.ListenAndServeTLS(serverCertPath, serverKeyPath)
	} else {
		err = server.ListenAndServe()
	}
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

func handlePrometheusWrite(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract org_id from client certificate or environment variable
		orgID, err := extractOrgID(r)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to determine organization ID: %v", err), http.StatusUnauthorized)
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
				OrgID:     orgID,
				Labels:    make(map[string]string),
				Timestamp: 0,
				Value:     0,
			}

			// Extract metric name from labels
			for _, label := range ts.Labels {
				if label.Name == "__name__" {
					metric.Name = label.Value
				} else {
					metric.Labels[label.Name] = label.Value
				}
			}

			// Extract timestamp and value from samples
			for _, sample := range ts.Samples {
				metric.Timestamp = sample.Timestamp
				metric.Value = sample.Value
			}

			// Convert labels to JSON for PostgreSQL JSONB
			labelsJSON, err := json.Marshal(metric.Labels)
			if err != nil {
				log.Printf("Failed to marshal labels: %v", err)
				continue
			}

			// Insert metric into database
			_, err = db.Exec(`
				INSERT INTO metrics (name, org_id, labels, timestamp, value)
				VALUES ($1, $2, $3, $4, $5)
			`, metric.Name, metric.OrgID, labelsJSON, metric.Timestamp, metric.Value)
			if err != nil {
				log.Printf("Failed to insert metric: %v", err)
				continue
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

func extractOrgID(r *http.Request) (string, error) {
	// Try to extract from certificate if TLS is used
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		cert := r.TLS.PeerCertificates[0]
		if len(cert.Subject.Organization) > 0 {
			return cert.Subject.Organization[0], nil
		}
		return "", fmt.Errorf("no organization in client certificate")
	}

	// Fallback to ORG_ID environment variable
	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		return "", fmt.Errorf("no client certificate provided and ORG_ID environment variable not set")
	}
	return orgID, nil
}
