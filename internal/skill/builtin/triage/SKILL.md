Each record is one support request: a line of text, a line of JSONL, an
element of a JSON array, or a row of CSV.

`urgent` is the probability that the request needs a response within
hours. `impact` is a score from 0 (no real impact) to 4 (critical).

A request is flagged when it is likely urgent, or when its impact is 3
(major) or higher.

To route requests to a queue as well, run `ticket-routing` on the same
data.
