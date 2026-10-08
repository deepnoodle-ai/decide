# Security tutorial: rank the demo's admin functions, then read candidates.
Set Height 988
Hide
Type "sh $REPO/demo/setup.sh shop >/dev/null && cd shop && clear" Enter
Wait
Show
Type "decide run security admin > /dev/null" Enter
Wait
Type `decide runs view --top 6 --json | jq -r '[.source, .input, ([.answers | to_entries[] | select(.value.noul >= 0.3) | .key] | join(", "))] | @tsv'` Enter
Wait
Sleep 5s
# Expect: Server.ExportOrders
# Expect: command_injection
