# /tutorials/check-agent-commands/, the last step: checked.sh runs a safe
# command, then refuses a force push. checked.sh comes from
# site/src/snippets, which the page shows, so the tape and the page agree.
Hide
Type "cp $REPO/site/src/snippets/checked.sh . && chmod +x checked.sh && clear" Enter
Wait
Show
Type "./checked.sh 'echo it ran'" Sleep 500ms Enter
Wait
Sleep 4s
Hide
Type "clear" Enter
Wait
Show
Type "./checked.sh 'git push --force origin main'" Sleep 500ms Enter
Wait
Type "echo $?" Sleep 300ms Enter
Wait
Sleep 5s
# Expect: it ran
# Expect: decide flagged this command; not running it
# Expect: nothing flagged
# Expect: ! destructive
# Expect: ! severe
