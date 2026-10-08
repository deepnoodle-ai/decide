# Go tutorial: the same source as examples/gate, in a fresh local module.
Set Height 390
Hide
Type "cp $REPO/examples/gate/main.go main.go && printf 'module example.com/gate\n\ngo 1.27\n\nrequire github.com/deepnoodle-ai/decide v0.4.0\n' > go.mod && go mod edit -replace=github.com/deepnoodle-ai/decide=$REPO && go mod tidy && clear" Enter
Wait
Show
Type "go run ." Enter
Wait
Sleep 5s
# Expect: action
