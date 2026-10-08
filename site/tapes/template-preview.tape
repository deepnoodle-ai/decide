# Custom-template tutorial: inspect the template and preview two tickets.
Set Height 1180
Hide
Type "mkdir -p .decide/templates/refund-check && cp $REPO/site/src/snippets/refund-check.json .decide/templates/refund-check/template.json && cp $REPO/site/src/snippets/refund-tickets.txt tickets.txt && clear" Enter
Wait
Show
Type "decide templates show refund-check" Enter
Wait
Type "decide run refund-check tickets.txt --dry-run" Enter
Wait
Sleep 5s
# Expect: project template
# Expect: would look at 2 lines
