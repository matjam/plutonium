package load

// The Go build installs the printer as types2.DeclPrinter here (printhook.go),
// so that the type checker can show the fix for an owner type declaration as
// source. The printer is not translated yet (self-hosting plan, section 8:
// it needs text/tabwriter and go/doc/comment), so the translation installs
// nothing, and the type checker shows the description of the fix instead.
