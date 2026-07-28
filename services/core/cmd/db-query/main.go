package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: db-query <query>")
		fmt.Println("Examples:")
		fmt.Println("  db-query 'SELECT * FROM recommendations LIMIT 5'")
		fmt.Println("  db-query 'SELECT COUNT(*) FROM players'")
		os.Exit(1)
	}

	db, err := sql.Open("sqlite3", "/data/fpl.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	query := os.Args[1]
	rows, err := db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		log.Fatal(err)
	}

	// Print header
	for i, col := range columns {
		if i > 0 {
			fmt.Print(" | ")
		}
		fmt.Print(col)
	}
	fmt.Println()
	fmt.Println("---")

	// Print rows
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range columns {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		err := rows.Scan(valuePtrs...)
		if err != nil {
			log.Fatal(err)
		}

		for i, val := range values {
			if i > 0 {
				fmt.Print(" | ")
			}
			if val == nil {
				fmt.Print("NULL")
			} else {
				fmt.Print(val)
			}
		}
		fmt.Println()
	}
}
