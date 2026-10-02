package billing

import "fmt"

// Line is one line of an invoice.
type Line struct {
	Description string
	Cents       int64
}

// Total adds up the lines of an invoice.
func Total(lines []Line) int64 {
	var total int64
	for _, line := range lines {
		total += line.Cents
	}
	return total
}

// Format shows cents as dollars.
func Format(cents int64) string {
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}
