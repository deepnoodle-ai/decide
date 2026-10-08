# Landing page: one real choice answer, using the example template beside it.
Set Width 1126
Set Height 650
Set TypingSpeed 15ms
Hide
Type "mkdir -p .decide/templates/ticket && cp $REPO/site/src/snippets/type-choice.json .decide/templates/ticket/template.json && cp $REPO/site/src/snippets/type-choice.txt ticket.txt && clear" Enter
Wait
Show
Type "decide run ticket ticket.txt" Enter
Wait
Sleep 3s
# Expect: queue
# Expect: ✓ 1 answered
