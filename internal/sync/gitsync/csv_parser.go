package gitsync

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

type RoomCSV struct {
	ID          string
	Name        string
	MinDuration string
	MaxDuration string
	IsActive    bool
	Description string
}

type BookCSV struct {
	ID          string
	Title       string
	Author      string
	Category    string
	TotalStock  string
	Description string
}

func ParseRoomsCSV(path string) ([]RoomCSV, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';'
	reader.FieldsPerRecord = -1 // Allow variable number of fields if needed, though we expect specific

	var rooms []RoomCSV
	lineCount := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading CSV line %d: %w", lineCount+1, err)
		}

		// Skip header if present
		if lineCount == 0 && len(record) > 0 && strings.ToLower(record[0]) == "id" {
			lineCount++
			continue
		}
		lineCount++

		if len(record) < 6 {
			// Skip malformed lines, or log? choosing to skip for now to be robust
			continue
		}

		rooms = append(rooms, RoomCSV{
			ID:          strings.TrimSpace(record[0]),
			Name:        strings.TrimSpace(record[1]),
			MinDuration: strings.TrimSpace(record[2]),
			MaxDuration: strings.TrimSpace(record[3]),
			IsActive:    strings.ToLower(strings.TrimSpace(record[4])) == "true" || record[4] == "1",
			Description: strings.TrimSpace(record[5]),
		})
	}

	return rooms, nil
}

func ParseBooksCSV(path string) ([]BookCSV, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';'
	reader.FieldsPerRecord = -1

	var books []BookCSV
	lineCount := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading CSV line %d: %w", lineCount+1, err)
		}

		// Skip header if present (check multiple fields to be sure)
		if lineCount == 0 && len(record) > 0 && strings.ToLower(record[0]) == "id" && strings.ToLower(record[1]) == "title" {
			lineCount++
			continue
		}
		lineCount++

		if len(record) < 6 {
			continue
		}

		books = append(books, BookCSV{
			ID:          strings.TrimSpace(record[0]),
			Title:       strings.TrimSpace(record[1]),
			Author:      strings.TrimSpace(record[2]),
			Category:    strings.TrimSpace(record[3]),
			TotalStock:  strings.TrimSpace(record[4]), // Will parse to int later
			Description: strings.TrimSpace(record[5]),
		})
	}

	return books, nil
}
