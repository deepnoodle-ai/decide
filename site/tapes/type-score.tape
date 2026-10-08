# Landing page: one real score answer, using the example template beside it.
Set Width 1126
Set Height 650
Set TypingSpeed 15ms
Hide
Type "mkdir -p .decide/templates/code && cp $REPO/site/src/snippets/type-score.json .decide/templates/code/template.json && cp $REPO/site/src/snippets/type-score.txt code.txt && clear" Enter
Wait
Show
Type "decide run code code.txt" Enter
Wait
Sleep 3s
# Expect: maintainability
# Expect: ✓ 1 answered
