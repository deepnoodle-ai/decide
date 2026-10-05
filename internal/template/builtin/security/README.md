Use it on server code, to find the functions most likely to hold a
common vulnerability. It asks every question in one request per function.

    decide run security src
    decide run security . --include '*.go' --exclude '*_test.go'
    git diff main | decide run security --each function

Each answer is the probability that the function has that weakness:

| Question | Weakness | CWE | OWASP Top 10:2025 |
| --- | --- | --- | --- |
| `sql_injection` | Untrusted input in the text of a SQL query | 89 | A05 Injection |
| `command_injection` | Untrusted input in a command, or an argument read as an option | 78, 77, 88 | A05 Injection |
| `ssrf` | Untrusted input chooses the address the server connects to | 918 | A01 Broken Access Control |
| `xss` | Untrusted input in HTML without encoding | 79 | A05 Injection |
| `weak_cipher` | DES, RC4, ECB mode, or another weak cipher | 327 | A04 Cryptographic Failures |
| `weak_hash` | MD5 or SHA-1 where security depends on it | 328 | A04 Cryptographic Failures |
| `weak_random` | A predictable random value used as a secret | 330 | A04 Cryptographic Failures |
| `tls_verification` | TLS certificate or host name checks turned off | 295 | A07 Authentication Failures |

Untrusted input is a value a remote user or another system controls.
Command-line flags, environment variables, and configuration files are
the operator's, and count as trusted.

A function is flagged when an injection, SSRF, or XSS answer is at least
50% likely, `weak_cipher` at least 90%, or another answer at least 60%.
On real bugs in Gogs (Go) and n8n (TypeScript), many injection and SSRF
bugs scored between 50% and 70%, so a higher threshold misses them. It
doesn't find every bug: about 4 in 10 of n8n's real SQL injection and
SSRF bugs scored under 50%. On the OWASP Benchmark, safe code scored up
to 84% on `weak_cipher`, and every real case 96% or more.

The model judges one function at a time, so it can't see whether a
caller passes a constant. Read the flagged functions with their callers,
and sort by probability to read the likeliest first:

    decide runs view --format csv > security.csv

Expect some flags that are intended: an option that turns off TLS checks
because an admin asked for it, or HMAC-SHA1 that a webhook's sender
requires. Code that fetches URLs its users name, such as an integration
platform, flags often for SSRF: in n8n, 374 of 10,530 functions.
