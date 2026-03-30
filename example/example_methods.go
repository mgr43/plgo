package main

import (
	"bytes"
	"compress/gzip"
	"log"
	"strings"

	"gitlab.com/microo8/plgo"
)

// Meh prints out a message to PostgreSQL's error log.
// This is a void function — it returns nothing (RETURNS VOID in SQL).
func Meh() {
	logger := plgo.NewErrorLogger("", log.Ltime|log.Lshortfile)
	logger.Println("meh")
}

// ConcatAll concatenates all values of a column in a given table.
// Demonstrates SPI database access: Open, Prepare, Query, Scan.
func ConcatAll(tableName, colName string) string {
	logger := plgo.NewErrorLogger("", log.Ltime|log.Lshortfile)
	db, err := plgo.Open()
	if err != nil {
		logger.Fatalf("Cannot open DB: %s", err)
	}
	defer db.Close()
	query := "select " + colName + " from " + tableName
	stmt, err := db.Prepare(query, nil)
	if err != nil {
		logger.Fatalf("Cannot prepare query statement (%s): %s", query, err)
	}
	rows, err := stmt.Query()
	if err != nil {
		logger.Fatalf("Query (%s) error: %s", query, err)
	}
	var ret string
	for rows.Next() {
		var val string
		cols, err := rows.Columns()
		if err != nil {
			logger.Fatalln("Cannot get columns", err)
		}
		logger.Println(cols)
		err = rows.Scan(&val)
		if err != nil {
			logger.Fatalln("Cannot scan value", err)
		}
		ret += val
	}
	return ret
}

// CreatedTimeTrigger is a trigger function that modifies rows on INSERT.
// Trigger functions must accept *plgo.TriggerData as the first parameter
// and return *plgo.TriggerRow.
func CreatedTimeTrigger(td *plgo.TriggerData) *plgo.TriggerRow {
	var id int
	var value string
	td.NewRow.Scan(&id, &value)
	td.NewRow.Set(0, id+10)
	td.NewRow.Set(1, value+value)
	return td.NewRow
}

// ConcatArray concatenates an array of strings.
// Demonstrates array parameter support (text[] in PostgreSQL).
func ConcatArray(strs []string) string {
	return strings.Join(strs, "")
}

// GzipCompress compresses binary data using gzip.
// Demonstrates []byte (bytea) parameter and return type,
// and using Go standard library packages inside PostgreSQL.
func GzipCompress(data []byte) []byte {
	logger := plgo.NewErrorLogger("", log.Ltime|log.Lshortfile)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(data)
	if err != nil {
		logger.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		logger.Fatal(err)
	}
	return buf.Bytes()
}

// DoubleInt doubles an integer value.
// Demonstrates int32 (integer) parameter and return type.
func DoubleInt(n int32) int32 {
	return n * 2
}

// ScaleArray multiplies every element in an integer array by a factor.
// Demonstrates []int64 (bigint[]) return type.
func ScaleArray(nums []int64, factor int64) []int64 {
	result := make([]int64, len(nums))
	for i, n := range nums {
		result[i] = n * factor
	}
	return result
}

// MaybeUpper returns the uppercased string, or NULL if the input is empty.
// Demonstrates nullable return values using pointer types.
func MaybeUpper(s string) *string {
	if s == "" {
		return nil
	}
	result := strings.ToUpper(s)
	return &result
}
