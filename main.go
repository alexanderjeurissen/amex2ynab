package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ColumnMapper helps map source columns to target columns
type ColumnMapper struct {
	DateColumns      []string
	PayeeColumns     []string
	AmountColumns    []string
	MemoColumns      []string
	ReferenceColumns []string
	LocationColumns  []string
	PostcodeColumns  []string
	CountryColumns   []string
}

func main() {
	// Define flags
	inputFilePath := flag.String("input", "", "Path to input CSV file (required)")
	// Get default output path with timestamp in the format ~/Desktop/ynab_amex_export_YYYYMMDDHHmmss.csv
	homeDir, _ := os.UserHomeDir()
	defaultOutputPath := filepath.Join(homeDir, "Desktop", fmt.Sprintf("ynab_amex_export_%s.csv", time.Now().Format("20060102150405")))
	outputFilePath := flag.String("output", defaultOutputPath, "Path to output CSV file")

	// Parse flags
	flag.Parse()

	// Check if input file path is provided
	if *inputFilePath == "" {
		fmt.Println("Error: input file path is required")
		flag.Usage()
		os.Exit(1)
	}

	// Read the input file
	inputFile, err := os.Open(*inputFilePath)
	if err != nil {
		log.Fatalf("Failed to open input file: %v", err)
	}
	defer inputFile.Close()

	// Create the output file
	outputFile, err := os.Create(*outputFilePath)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer outputFile.Close()

	// Process the CSV
	if err := processCSV(inputFile, outputFile); err != nil {
		log.Fatalf("Failed to process CSV: %v", err)
	}

	fmt.Printf("Successfully converted %s to YNAB format. Output saved to %s\n", *inputFilePath, *outputFilePath)
}

func processCSV(inputFile io.Reader, outputFile io.Writer) error {
	// Create CSV readers and writers
	reader := csv.NewReader(inputFile)
	writer := csv.NewWriter(outputFile)
	defer writer.Flush()

	// Read the header
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Create a column mapper
	mapper := createColumnMapper()

	// Find index of each required column
	dateIdx := findColumnIndex(header, mapper.DateColumns)
	payeeIdx := findColumnIndex(header, mapper.PayeeColumns)
	amountIdx := findColumnIndex(header, mapper.AmountColumns)
	memoIdx := findColumnIndex(header, mapper.MemoColumns)
	referenceIdx := findColumnIndex(header, mapper.ReferenceColumns)
	locationIdx := findColumnIndex(header, mapper.LocationColumns)
	postcodeIdx := findColumnIndex(header, mapper.PostcodeColumns)
	countryIdx := findColumnIndex(header, mapper.CountryColumns)

	// Check if required columns were found
	if dateIdx == -1 || payeeIdx == -1 || amountIdx == -1 {
		return fmt.Errorf("required columns not found in the CSV file")
	}

	// Write YNAB header
	err = writer.Write([]string{"Date", "Payee", "Memo", "Amount"})
	if err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Process each row
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read row: %w", err)
		}

		// Extract and format date
		date := formatDate(row[dateIdx])

		// Extract payee
		payee := row[payeeIdx]

		// Build memo from additional info and reference
		var memoBuilder strings.Builder

		if memoIdx != -1 && row[memoIdx] != "" {
			memoBuilder.WriteString(row[memoIdx])
		}

		// Add reference if available
		if referenceIdx != -1 && row[referenceIdx] != "" {
			if memoBuilder.Len() > 0 {
				memoBuilder.WriteString(" | ")
			}
			memoBuilder.WriteString("Ref: ")
			memoBuilder.WriteString(row[referenceIdx])
		}

		// Add location information if available
		var location strings.Builder
		if locationIdx != -1 && row[locationIdx] != "" {
			location.WriteString(row[locationIdx])
		}
		if postcodeIdx != -1 && row[postcodeIdx] != "" {
			if location.Len() > 0 {
				location.WriteString(", ")
			}
			location.WriteString(row[postcodeIdx])
		}
		if countryIdx != -1 && row[countryIdx] != "" {
			if location.Len() > 0 {
				location.WriteString(", ")
			}
			location.WriteString(row[countryIdx])
		}

		// Add location to memo
		if location.Len() > 0 {
			if memoBuilder.Len() > 0 {
				memoBuilder.WriteString(" | ")
			}
			memoBuilder.WriteString("Location: ")
			memoBuilder.WriteString(location.String())
		}

		memo := memoBuilder.String()

		// Extract and invert amount
		amount := invertAmount(row[amountIdx])

		// Write the YNAB row
		err = writer.Write([]string{date, payee, memo, amount})
		if err != nil {
			return fmt.Errorf("failed to write row: %w", err)
		}
	}

	return nil
}

func createColumnMapper() ColumnMapper {
	return ColumnMapper{
		DateColumns:      []string{"Datum"},
		PayeeColumns:     []string{"Omschrijving"},
		AmountColumns:    []string{"Bedrag"},
		MemoColumns:      []string{"Aanvullende informatie"},
		ReferenceColumns: []string{"Referentie"},
		LocationColumns:  []string{"Plaats"},
		PostcodeColumns:  []string{"Postcode"},
		CountryColumns:   []string{"Land"},
	}
}

func findColumnIndex(header []string, possibleNames []string) int {
	for i, h := range header {
		h = strings.TrimSpace(strings.ToLower(h))
		for _, name := range possibleNames {
			if strings.TrimSpace(strings.ToLower(name)) == h {
				return i
			}
		}
	}
	return -1
}

func formatDate(dateStr string) string {
	// Try different date formats
	formats := []string{
		"02-01-2006",      // DD-MM-YYYY
		"02/01/2006",      // DD/MM/YYYY
		"2006-01-02",      // YYYY-MM-DD
		"01/02/2006",      // MM/DD/YYYY
		"January 2, 2006", // Month D, YYYY
		"2 January 2006",  // D Month YYYY
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t.Format("2006-01-02") // YYYY-MM-DD
		}
	}

	// If no format matches, return the original string
	// This is not ideal but allows the process to continue
	return dateStr
}

func invertAmount(amountStr string) string {
	// Remove currency symbols, spaces, and handle European decimal format
	re := regexp.MustCompile(`[^\d.,\-]`)
	cleanAmount := re.ReplaceAllString(amountStr, "")

	// Replace comma with dot if European format
	if strings.Count(cleanAmount, ",") == 1 && strings.Count(cleanAmount, ".") <= 1 {
		// If there are both commas and dots, assume European format with thousands separator
		if strings.Count(cleanAmount, ".") == 1 {
			cleanAmount = strings.ReplaceAll(cleanAmount, ".", "")
		}
		cleanAmount = strings.Replace(cleanAmount, ",", ".", 1)
	}

	// Parse the amount
	amount, err := strconv.ParseFloat(cleanAmount, 64)
	if err != nil {
		// Return original if parsing fails
		return amountStr
	}

	// Invert the amount
	invertedAmount := -amount

	// Format the result with 2 decimal places
	return fmt.Sprintf("%.2f", invertedAmount)
}
