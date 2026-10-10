package diag

// The explanations of the ownership and lifetime rules (language spec
// section 6). The errors themselves carry explanations specialised to the
// code at fault, built where they are found; these texts are what pluto
// explain prints, and what an error prints without one of its own.

// whyFrame is why a view of the frame cannot outlive it.
const whyFrame = "Go would move this memory to the heap, and its garbage collector would free it later. Plutonium has no garbage collector and never moves memory to the heap behind the program's back, so that every allocation is visible in the code."

func init() {
	register(
		&Entry{
			Code: ReturnFrameView, Name: "ReturnFrameView",
			Text: "A function returns a view of its own frame: a pointer to one of its local variables, a slice of a local array, a function literal that uses local variables, or an interface holding a copy of a value in the frame. The frame is reclaimed when the function returns, so the caller would get a view of memory that no longer holds the value.\nReturn the value itself, which copies it, or moves it if it is an owner. To keep it at a fixed address after the function returns, allocate it.",
			Why:  whyFrame,
			Spec: pspec("6.3", "Views"),
			Example: "func load() *Config {\n\tvar c Config\n\tc.timeout = 30\n\treturn &c\n}",
			Fixed:   "func load() Config {\n\tvar c Config\n\tc.timeout = 30\n\treturn c\n}",
		},
		&Entry{
			Code: StoreFrameView, Name: "StoreFrameView",
			Text: "A view of the frame, such as a pointer to a local variable, is stored in memory that lives longer than the frame: a package variable, memory reached through a pointer the function was given, or memory that may be on the heap. When the function returns, that memory would still point at the reclaimed frame.\nStore the value instead of a pointer to it, or allocate the value so that it lives as long as the place that keeps it.",
			Why:  whyFrame,
			Spec: pspec("6.3", "Views"),
			Example: "var last *Entry\n\nfunc add(e Entry) {\n\tlast = &e\n}",
			Fixed:   "var last Entry\n\nfunc add(e Entry) {\n\tlast = e\n}",
		},
		&Entry{
			Code: PassFrameView, Name: "PassFrameView",
			Text: "A view of the frame is passed to a function that keeps it after returning: the function stores it in a package variable, in memory that outlives the call, or passes it on to something that does. The compiler knows this from the function's body, so no annotation is needed. Passing a view to a function that only uses it during the call is always allowed.\nGive the function something that owns its memory, such as a buffer or a value allocated for it.",
			Why:  whyFrame,
			Spec: pspec("6.3", "Views"),
		},
		&Entry{
			Code: GoFrameView, Name: "GoFrameView",
			Text: "A go statement starts a puroutine that runs on its own, and may still be running after the function that started it returns. A view of that function's frame passed to it could then point at reclaimed memory.\nPass the value itself, which copies it, or moves it if it is an owner.",
			Why:  whyFrame,
			Spec: pspec("6.3", "Views"),
			Example: "go handle(&req)",
			Fixed:   "go handle(req)",
		},
		&Entry{
			Code: SendFrameView, Name: "SendFrameView",
			Text: "A value sent on a channel may be received after the function that sent it returns. A view of that function's frame sent this way could then point at reclaimed memory.\nSend the value itself, which copies it, or moves it if it is an owner.",
			Why:  whyFrame,
			Spec: pspec("6.3", "Views"),
			Example: "ch <- &req",
			Fixed:   "ch <- req",
		},
		&Entry{
			Code: LoopFrameView, Name: "LoopFrameView",
			Text: "A pointer to a variable declared in a loop, or a function literal that uses one, is kept after the iteration that made it: stored in a variable declared outside the loop body, for example. Each iteration reuses the memory of the loop's variables, so what was kept would see the values of later iterations instead of its own.\nUse the pointer or call the function literal within the iteration, store values instead, or pass the value to the function literal as a parameter.",
			Why:  "Go gives each iteration of a loop its own variables, so a function literal made in one iteration keeps that iteration's values. Plutonium keeps variables in the function's frame, which has room for one copy of each; giving every iteration its own copy would need memory for each iteration, allocated behind the program's back.",
			Spec: pspec("4.5", "Closures do not outlive their frame"),
			Example: "var first func() int\nfor i := 0; i < 3; i++ {\n\tif i == 0 {\n\t\tfirst = func() int { return i * 10 }\n\t}\n}",
			Fixed:   "var first int\nfor i := 0; i < 3; i++ {\n\tif i == 0 {\n\t\tfirst = i * 10\n\t}\n}",
		},
		&Entry{
			Code: BadResultHolds, Name: "BadResultHolds",
			Text: "//pu:resultholds p says that a function keeps its parameter p only for as long as its result lives: the result, an owner, holds p and gives it back when it is dropped, as alloc.Use's Restore does. The compiler then treats the call as returning p instead of keeping it. The directive must name a parameter, and the function must store that parameter in its result.",
			Spec: pspec("9", "Directives"),
			Example: "//pu:resultholds a\nfunc Use(a Allocator) Restore {\n\tSetDefault(a)\n\treturn Restore{}\n}",
			Fixed:   "//pu:resultholds a\nfunc Use(a Allocator) Restore {\n\tr := Restore{prev: Default(), cur: a}\n\tSetDefault(a)\n\treturn r\n}",
		},
	)
}
