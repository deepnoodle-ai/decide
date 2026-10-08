# Custom-template tutorial: the same template and labeled tickets as the page.
Set Height 1180
Hide
Type "mkdir -p .decide/templates/refund-check && cp $REPO/site/src/snippets/refund-check.json .decide/templates/refund-check/template.json && cp $REPO/site/src/snippets/refund-tickets.txt tickets.txt && clear" Enter
Wait
Show
Type "decide run refund-check tickets.txt --fail-on flagged" Enter
Wait
Type "echo exit=$?" Enter
Wait
Sleep 5s
# Expect: ! 1 flagged
# Expect: exit=2
