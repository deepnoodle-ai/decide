# /tutorials/check-agent-commands/, step 1: two commands, both destructive,
# one of them severe.
Type "printf '%s\n' 'git reset --hard HEAD~3' 'rm -rf ~/' | decide run command-risk" Sleep 500ms Enter
Wait
Sleep 5s
# Expect: stdin:1  git reset --hard HEAD~3
# Expect: severe       no
# Expect: ! severe       yes
# Expect: ! 2 flagged
# Expect: Flagged: stdin:1, stdin:2
