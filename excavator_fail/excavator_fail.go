package fail

fail

/*
This is a non-compiling file that has been added to explicitly ensure that CI fails.
It also contains the command that caused the failure and its output.
Remove this file if debugging locally.

./godelw verify failed after updating godel plugins and assets

Command that caused error:
./godelw exec -- go fix ./appconfig ./example ./githubapp ./oauth2

Output:
# github.com/palantir/go-githubapp/githubapp
# [github.com/palantir/go-githubapp/githubapp]
fix: applied 6 of 7 fixes; 3 files updated. (Re-run the command to apply more.)
Error: exit status 1

*/
