Each PDF is one item. PDF templates run on Gemini Decision, so set
`GEMINI_API_KEY` first. A PDF that is not an invoice, or that lacks the
invoice number, seller, total, or due date, is flagged. `paid` is asked but
not flagged, so you can sort paid invoices out of a run. A scanned invoice
saved as an image works too when you name it, on a provider that reads
images: `decide run invoice-check scan.png --provider openai`.
