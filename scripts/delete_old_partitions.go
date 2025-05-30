package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

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

	// Define cutoff date (90 days before current date)
	currentDate := time.Date(2025, time.May, 27, 0, 0, 0, 0, time.UTC)
	cutoffDate := currentDate.AddDate(0, 0, -90)

	// Process each table
	tables := []string{"metrics", "daily_system_cpu_logical_count"}
	for _, table := range tables {
		// Query to find all partitions
		rows, err := db.Query(`
			SELECT child.relname
			FROM pg_inherits
			JOIN pg_class parent ON pg_inherits.inhparent = parent.oid
			JOIN pg_class child ON pg_inherits.inhrelid = child.oid
			WHERE parent.relname = $1
		`, table)
		if err != nil {
			log.Printf("Failed to query partitions for %s: %v", table, err)
			continue
		}
		defer rows.Close()

		// Collect partitions to drop
		var partitionsToDrop []string
		for rows.Next() {
			var partitionName string
			if err := rows.Scan(&partitionName); err != nil {
				log.Printf("Failed to scan partition name for %s: %v", table, err)
				continue
			}

			// Extract date from partition name (e.g., metrics_20250527)
			if !strings.HasPrefix(partitionName, table+"_") {
				log.Printf("Skipping invalid partition name: %s", partitionName)
				continue
			}

			dateStr := strings.TrimPrefix(partitionName, table+"_")
			if len(dateStr) != 8 {
				log.Printf("Skipping invalid partition date format: %s", partitionName)
				continue
			}

			partitionDate, err := time.Parse("20060102", dateStr)
			if err != nil {
				log.Printf("Failed to parse date from partition %s: %v", partitionName, err)
				continue
			}

			// Check if partition is older than cutoff date
			if partitionDate.Before(cutoffDate) {
				partitionsToDrop = append(partitionsToDrop, partitionName)
			}
		}

		if err := rows.Err(); err != nil {
			log.Printf("Error iterating partitions for %s: %v", table, err)
			continue
		}

		// Drop old partitions
		for _, partitionName := range partitionsToDrop {
			query := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", partitionName)
			_, err := db.Exec(query)
			if err != nil {
				log.Printf("Failed to drop partition %s: %v", partitionName, err)
				continue
			}
			log.Printf("Dropped partition %s", partitionName)
		}

		if len(partitionsToDrop) == 0 {
			log.Printf("No partitions older than %s found for %s", cutoffDate.Format("2006-01-02"), table)
		} else {
			log.Printf("Dropped %d partitions older than %s for %s", len(partitionsToDrop), cutoffDate.Format("2006-01-02"), table)
		}
	}
}
