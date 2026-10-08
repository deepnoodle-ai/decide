# Landing page: one real noul answer, using the example template beside it.
Set Width 1126
Set Height 650
Set TypingSpeed 15ms
Hide
Type "mkdir -p .decide/templates/command && cp $REPO/site/src/snippets/type-noul.json .decide/templates/command/template.json && cp $REPO/site/src/snippets/type-noul.txt command.txt && clear" Enter
Wait
Show
Type "decide run command command.txt" Enter
Wait
Sleep 3s
# Expect: destructive
# Expect: ✓ 1 answered
