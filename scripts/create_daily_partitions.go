package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
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

	// Define date range (May 27, 2025 to August 25, 2025)
	startDate := time.Date(2025, time.May, 27, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 0, 90)

	// Create partitions for each table
	tables := []string{"metrics", "daily_system_cpu_logical_count"}
	for _, table := range tables {
		currentDate := startDate
		for !currentDate.After(endDate) {
			nextDate := currentDate.AddDate(0, 0, 1)
			partitionName := fmt.Sprintf("%s_%s", table, currentDate.Format("20060102"))

			query := fmt.Sprintf(`
				CREATE TABLE IF NOT EXISTS %s PARTITION OF %s
				FOR VALUES FROM ('%s') TO ('%s');
			`, partitionName, table, currentDate.Format("2006-01-02"), nextDate.Format("2006-01-02"))

			_, err := db.Exec(query)
			if err != nil {
				log.Printf("Failed to create partition %s: %v", partitionName, err)
				continue
			}

			log.Printf("Created partition %s for %s", partitionName, currentDate.Format("2006-01-02"))
			currentDate = nextDate
		}
	}

	log.Printf("Created partitions for %v from %s to %s", tables, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
}
