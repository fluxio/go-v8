package v8

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"
)

func TestCreateV8Context(t *testing.T) {
	ctx := NewContext()
	res, err := ctx.Eval(`
        var a = 10;
        var b = 20;
        var c = a + b;
        c;
    `, NO_FILE)
	if err != nil {
		t.Error("Error evaluating javascript, err: ", err)
	}

	if res.(float64) != 30 {
		t.Error("Expected 30, got ", res)
	}
}

func TestAddFunc(t *testing.T) {
	ctx := NewContext()
	logFunc := "_console_log"
	ctx.AddFunc(logFunc, func(args ...interface{}) interface{} {
		for i, arg := range args {
			fmt.Printf("  Arg %d (%T): %#v\n", i, arg, arg)
		}
		return nil
	})

	_, present := ctx.funcs[logFunc]
	if !present {
		t.Error("Expected function %v to be present but was not", logFunc)
	}

	ctx.Eval(`
        _console_log("Added console.log function", 5);
    `, NO_FILE)
}

func v8Recurse(ctx *V8Context, t *testing.T) func(args ...interface{}) interface{} {
	return func(args ...interface{}) interface{} {
		arg0 := args[0].(float64)
		if arg0 > 0 {
			res, err := ctx.Run("'x' + recurse", arg0-1)
			if err != nil {
				t.Fatal(err)
			}
			return res.(string)
		}
		return "y"
	}
}

func TestParallelV8Contexts(t *testing.T) {
	done := make(chan error)
	NUM_VMS := 100
	SLEEP_DURATION := 30 * time.Millisecond
	fmt.Println("Starting", NUM_VMS, "VMs")
	startTime := time.Now()
	for i := 0; i < NUM_VMS; i++ {
		go func() {
			// Separate isolates to allow concurrent running: it's only one
			// thread per isolate.
			ctx := NewContextInIsolate(NewIsolate())
			ctx.AddFunc("sleep", func(args ...interface{}) interface{} {
				time.Sleep(SLEEP_DURATION)
				return nil
			})
			_, err := ctx.Run("sleep")
			done <- err
		}()
	}
	fmt.Println("All VMs started, waiting")
	for i := 0; i < NUM_VMS; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(startTime)
	fmt.Println("Done in ", elapsed)
	if elapsed < SLEEP_DURATION {
		t.Fatal("Expected to sleep for at least", SLEEP_DURATION)
	}
}

func TestManyV8RunsInSameContext(t *testing.T) {
	ctx := NewContext()
	startTime := time.Now()
	for i := 0; i < 1000; i++ {
		_, err := ctx.Eval("function sum(x, y) {return x + y};", NO_FILE)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ctx.Run("sum", i, i*2)
		if err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(startTime)
	fmt.Println("Done in ", elapsed)
}

func TestManyV8Contexts(t *testing.T) {
	startTime := time.Now()
	for i := 0; i < 1000; i++ {
		ctx := NewContext()
		_, err := ctx.Eval("function sum(x, y) {return x + y};", NO_FILE)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ctx.Run("sum", i, i*2)
		if err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(startTime)
	fmt.Println("Done in ", elapsed)
}

func TestRecursiveCallsIntoV8(t *testing.T) {
	ctx := NewContext()
	ctx.AddFunc("recurse", v8Recurse(ctx, t))
	result, err := ctx.Run("recurse", 5)
	if err != nil {
		t.Fatal(err)
	}

	if result != "xxxxxy" {
		t.Fatal("Got %v instead", result)
	}
	fmt.Println("Got:", result)
}

var _ = time.Time{}

func TestV8PingPong(t *testing.T) {
	iso := NewIsolate()
	vm1, vm2 := NewContext(), NewContextInIsolate(iso)

	ball := make(chan string)

	pingpong := func(args ...interface{}) interface{} {
		done := args[0].(bool)
		v := <-ball

		if done {
			ball <- "done"
			return "done"
		}

		if v == "done" {
			return "done"
		}

		if v == "ping" {
			ball <- "pong"
		} else {
			ball <- "ping"
		}

		return v
	}

	vm1.AddFunc("pingpong", pingpong)
	vm2.AddFunc("pingpong", pingpong)

	out := make(chan string)

	done1 := make(chan bool)
	done2 := make(chan bool)
	go func() {
		<-done1
		<-done2
		close(out)
	}()

	go func() {
		for i := 0; i < 10; i++ {
			res, err := vm1.Run("pingpong", false)
			if err != nil {
				t.Fatal(err)
			}
			out <- "VM1:" + res.(string)
		}
		_, err := vm1.Run("pingpong", true)
		if err != nil {
			t.Fatal(err)
		}
		done1 <- true
	}()
	go func() {
		for {
			res, err := vm2.Run("pingpong", false)
			if err != nil {
				t.Fatal(err)
			}
			out <- "VM2:" + res.(string)
			if res == "done" {
				break
			}
		}
		done2 <- true
	}()

	ball <- "ping"

	fmt.Print("Ball: ")
	for r := range out {
		fmt.Print(r, " | ")
	}
	fmt.Println("DONE")
}

func TestMultipleGoroutinesAccessingContext(t *testing.T) {
	NUM_GOROUTINES := 100
	runtime.GOMAXPROCS(NUM_GOROUTINES)

	ctx := NewContext()
	ctx.AddFunc("sleep", func(args ...interface{}) interface{} {
		time.Sleep(1 * time.Millisecond)
		return nil
	})

	results := make(chan string)
	for i := 0; i < NUM_GOROUTINES; i++ {
		go func() {
			ctx.Run("sleep")
			results <- "x"
		}()
	}

	for i := 0; i < NUM_GOROUTINES; i++ {
		<-results
	}
}

func ExampleAddConsoleLog() {
	ctx := NewContext()
	logFunc := "_console_log"
	ctx.AddFunc(logFunc, func(args ...interface{}) interface{} {
		for _, arg := range args {
			fmt.Printf("%v", arg)
		}
		return nil
	})
	ctx.Eval(`
        this.console = { "log": function(args) { _console_log(args) } };
        console.log("Example of logging to console");
    `, NO_FILE)
	// Output: Example of logging to console
}

func TestJSReferenceError(t *testing.T) {
	ctx := NewContext()
	_, err := ctx.Eval(`
        dne; // dne = does not exist.  Should cause error in v8.
    `, "my_file.js")

	t.Logf("Error: [%v]", err)

	match, _ := regexp.MatchString(
		"Stack trace: ReferenceError: dne is not defined.*\n.*at my_file.js",
		err.Error())
	if !match {
		t.Error("Expected 'ReferenceError' in error string, got: ", err)
	}
}

func TestSyntaxError(t *testing.T) {
	ctx := NewContext()
	_, err := ctx.Eval(`
		// blah blah blah
		function(asdf) { // error, no function name
			return 0
		}
	`, "my_file.js")

	t.Logf("Err is:\n%v\n", err)

	if !strings.Contains(err.Error(), `Uncaught exception`) ||
		!strings.Contains(err.Error(), `SyntaxError`) ||
		!strings.Contains(err.Error(), `Unexpected token`) ||
		!strings.Contains(err.Error(), `my_file.js:3:10`) ||
		!strings.Contains(err.Error(), `function(asdf)`) {
		t.Errorf("Failed to find expected substrings in error.")
	}
}

func TestJSTypeError(t *testing.T) {
	ctx := NewContext()
	_, err := ctx.Eval(`
		var x = undefined;
        x.blah;
    `, "my_file.js")

	t.Logf("Error: [%v]", err)

	match, _ := regexp.MatchString(
		"Stack trace: TypeError: Cannot read property 'blah' of undefined.*\n.*at my_file.js",
		err.Error())
	if !match {
		t.Error("Expected 'TypeError' in error string, got: ", err)
	}
}

func TestJSThrowString(t *testing.T) {
	ctx := NewContext()
	_, err := ctx.Eval(`throw 'badness'`, "my_file.js")
	t.Logf("Error: [%v]", err)
	match, _ := regexp.MatchString("Uncaught exception: badness", err.Error())
	if !match {
		t.Error("Expected 'Uncaught exception: badness'")
	}
}

func TestJSThrowObject(t *testing.T) {
	ctx := NewContext()
	_, err := ctx.Eval(`throw {msg:"died", data:3}`, "my_file.js")
	t.Logf("Error: [%v]", err)
	match, _ := regexp.MatchString("Uncaught exception:.*died", err.Error())
	if !match {
		t.Error("Expected 'Uncaught exception: ... died ...'")
	}
}

func TestTerminate(t *testing.T) {
	ctx := NewContext()

	// Run an infinite loop in a goroutine.
	done := make(chan bool)
	go func() {
		fmt.Println("Running infinite loop:")
		ctx.Eval("while(1){}", NO_FILE)
		fmt.Println("Completed infinite loop!")
		done <- true
	}()

	// Sleep for a bit to make sure that it's
	fmt.Println("Verifying that v8 is spinning")
	select {
	case <-done:
		t.Fatal("V8 infinite loop failed to loop infinitely")
	case <-time.After(10 * time.Millisecond):
	}

	fmt.Println("Interrupting V8")
	ctx.Terminate()
	fmt.Println("Waiting for infinite loop")
	<-done

	// Make sure you can call terminate when nothing is running, and it
	// works alright.
	ctx.Terminate()
	fmt.Println("I didn't crash!")
}

// Verify that terminate doesn't affect other running vms.
func TestTerminateOnlySpecificVM(t *testing.T) {
	runloop := func(vm *V8Context) chan bool {
		done := make(chan bool)
		go func() {
			vm.Eval("while(1){}", NO_FILE)
			done <- true
		}()
		return done
	}

	vm1, vm2 := NewContext(), NewContextInIsolate(NewIsolate())
	done1, done2 := runloop(vm1), runloop(vm2)

	select {
	case <-done1:
		t.Fatal("Premature loop exit")
	case <-done2:
		t.Fatal("Premature loop exit")
	case <-time.After(10 * time.Millisecond):
	}

	vm2.Terminate()
	<-done2

	select {
	case <-done1:
		t.Fatal("Premature loop exit")
	case <-time.After(10 * time.Millisecond):
	}
	vm1.Terminate() // Stop the infinite loop.
}

func TestRunningCodeInContextAfterThrowingError(t *testing.T) {
	ctx := NewContext()
	_, err := ctx.Eval(`
		function fail(a,b) {
			this.c = a+b;
			throw "some failure";
		}
		function work(a,b) {
			this.c = a+b+2;
		}
		x = new fail(3,5);`, "file1.js")
	if err == nil {
		t.Fatal("Expected an exception.")
	}

	res, err := ctx.Eval(`y = new work(3,6); y.c`, "file2.js")
	if err != nil {
		t.Fatal("Expected it to work, but got:", err)
	}

	if res.(float64) != 11 {
		t.Errorf("Expected 11, got: %#V", res)
	}
}

func TestManyContextsThrowingErrors(t *testing.T) {
	prog := `
		function work(N, depth, fail) {
			if (depth == 0) { return 1; }
			var sum = 0;
			for (i = 0; i < N; i++) { sum *= work(N, depth-1); }
			if (fail) {
				throw "Failed";
			}
			return sum;
		}`

	const N = 100 // num parallel contexts
	runtime.GOMAXPROCS(N)

	var done sync.WaitGroup
	var ctxs []*V8Context

	done.Add(N)
	for i := 0; i < N; i++ {
		ctx := NewContext()
		ctx.Eval(prog, "prog.js")
		ctxs = append(ctxs, ctx)
		go func(ctx *V8Context, i int) {
			ctx.Run("work", 100000, 100, (i%5 == 0))
			ctx.Run("work", 100000, 100, (i%5 == 0))
			ctx.Run("work", 100000, 100, (i%5 == 0))
			done.Done()
		}(ctx, i)
	}
	done.Wait()
}

func TestErrorsInNativeCode(t *testing.T) {
	ctx := NewContext()
	_, err := ctx.Eval(`[].map(undefined);`, "map_undef.js")
	if err == nil {
		t.Fatal("Expected error.")
	}
	t.Log("Got expected error: ", err)
}

func TestStackOverflow(t *testing.T) {
	// TODO(aroman) There's a way to handle this gracefully.
	t.Skip("Need to figure out how to handle stack overflow.")

	ctx := NewContext()
	_, err := ctx.Eval(`function a(x,y) { return a(x,y) + a(y,x); }; a(1,2)`,
		"stack_attack.js")
	if err == nil {
		t.Fatal("Expected error.")
	}
	t.Log("Got expected error: ", err)
}

func TestRunFunc(t *testing.T) {
	ctx := NewContext()
	_, err := ctx.Eval(`function add(a,b,c) { return a+b.Val+c[0]+c[1]; }`, NO_FILE)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ctx.Run("add", 3, struct{ Val int }{5}, []int{7, 13})
	if err != nil {
		t.Fatal(err)
	}
	resnum, ok := res.(float64)
	if !ok {
		t.Fatalf("Expected to get a number out of v8, got: %#V", res)
	}
	if resnum != 28 {
		t.Fatal("Expected 28, got ", resnum)
	}
}

func TestEvalRaw(t *testing.T) {
	ctx := NewContext()

	val, err := ctx.EvalRaw(`x = {a:3,b:{c:'asdf'}}`, "new eval hotness!")
	if err != nil {
		t.Fatal(err)
	}

	jsonstr := val.ToJSON()
	expected := `{"a":3,"b":{"c":"asdf"}}`
	if jsonstr != expected {
		t.Fatalf("JSON mismatch:\n  Expected:'%s'\n       Got: '%s'", expected, jsonstr)
	}
}

func TestFromJson(t *testing.T) {
	ctx := NewContext()
	json := `{"a":3,"b":{"c":"asdf"}}`
	val, err := ctx.FromJSON(json)
	if err != nil {
		t.Fatal(err)
	}
	if val.ToJSON() != json {
		t.Fatalf("JSON mismatch:\n  Expected:'%s'\n       Got: '%s'", json, val.ToJSON())
	}
}

func TestApply(t *testing.T) {
	ctx := NewContext()

	must := func(val *Value, e error) *Value {
		if e != nil {
			t.Fatal(e)
		}
		return val
	}

	f := must(ctx.CreateJS(`function(a,b) { return a+b; }`, NO_FILE))
	a := must(ctx.CreateJS(`3`, NO_FILE))
	b := must(ctx.CreateJS(`7`, NO_FILE))

	res := must(ctx.Apply(f, f, a, b))
	t.Log(res)
	if res.ToJSON() != "10" {
		t.Fatal("Expected 10, got ", res.ToJSON())
	}
}

// Test "apply" when we manually specify the "this" context for the func.
func TestApplyWithThis(t *testing.T) {
	ctx := NewContext()

	must := func(val *Value, e error) *Value {
		if e != nil {
			t.Fatal(e)
		}
		return val
	}

	f := must(ctx.CreateJS(`function(x) { return x+this.y; }`, NO_FILE))
	x := must(ctx.CreateJS(`5`, NO_FILE))
	y1 := must(ctx.CreateJS(`{y:3}`, NO_FILE))
	y2 := must(ctx.CreateJS(`{y:7}`, NO_FILE))

	res := must(ctx.Apply(f, y1, x))
	t.Log(res)
	if res.ToJSON() != "8" {
		t.Fatal("Expected 8, got ", res.ToJSON())
	}

	res = must(ctx.Apply(f, y2, x))
	t.Log(res)
	if res.ToJSON() != "12" {
		t.Fatal("Expected 12, got ", res.ToJSON())
	}
}

func TestRawFunc(t *testing.T) {
	ctx := NewContext()

	callback := func(src Loc, args ...*Value) (*Value, error) {
		if src.Filename != "some_test_file.js" {
			t.Errorf("Wrong calling filename in callback: %q", src.Filename)
		}
		return args[len(args)-1], nil
	}
	ctx.AddRawFunc("lastarg", callback)

	arg, err := ctx.EvalRaw("lastarg(1,2,3,4,'blah')", "some_test_file.js")
	if err != nil {
		t.Fatal(err)
	}
	if arg.ToJSON() != `"blah"` {
		t.Fatal("Expected '\"blah\"', got ", arg.ToJSON())
	}

	arg, err = ctx.EvalRaw("lastarg(1,2,3,4)", "some_test_file.js")
	if err != nil {
		t.Fatal(err)
	}
	if arg.ToJSON() != `4` {
		t.Fatal("Expected '4', got ", arg.ToJSON())
	}
}

func TestRawFuncReturnNull(t *testing.T) {
	ctx := NewContext()

	ctx.AddRawFunc("undef", func(_ Loc, args ...*Value) (*Value, error) { return nil, nil })
	arg, err := ctx.EvalRaw("undef(1,2,3)", "test")
	if err != nil {
		t.Fatal("error", err)
	}
	if arg.ToJSON() != "undefined" {
		t.Fatal("Expected undefined, got ", arg.ToJSON())
	}
}

func TestRawFuncResultError(t *testing.T) {
	ctx := NewContext()

	ctx.AddRawFunc("die", func(_ Loc, args ...*Value) (*Value, error) {
		return nil, fmt.Errorf("diediedie")
	})
	arg, err := ctx.EvalRaw("die(1,2,3)", "test")
	if err == nil || arg != nil {
		t.Error("Expected an error result, got:\n  Val: %v\n  Err: %v", arg, err)
	}
	if !strings.Contains(err.Error(), `diediedie`) {
		t.Errorf(`Expected an exception containing "diediedie", got:\n%v`, err)
	}
}

func TestRawFuncOrigin(t *testing.T) {
	ctx := NewContext()
	ctx.AddRawFunc("require", func(src Loc, args ...*Value) (*Value, error) {
		// All calls are from the single inner.js call below, regardless of
		// which script initiated the call to inner().
		if src != (Loc{"inner", "inner.js", 3, 4}) {
			t.Errorf("Wrong caller location provided to raw function callback: %#v", src)
		}
		return nil, nil
	})
	ctx.Eval(`
		function inner() {
			require('foo');
		};
		inner();`, "inner.js")
	ctx.Eval(`
		function middle() {
			inner();
		};
		inner();
		middle();`, "middle.js")
	ctx.Eval(`
		inner();
		middle();`, "outer.js")
}

func TestValueToString(t *testing.T) {
	ctx := NewContext()

	val, err := ctx.EvalRaw(`"lalala"`, "test")
	if err != nil {
		t.Fatal(err)
	}
	str, err := val.ToString()
	if err != nil {
		t.Fatal(err)
	}
	if str != "lalala" {
		t.Fatalf("Expected 'lalala', got '%s'", str)
	}

	val, err = ctx.EvalRaw(`123`, "test")
	if err != nil {
		t.Fatal(err)
	}
	str, err = val.ToString()
	if err == nil {
		t.Fatalf("Expected error, but got nil and the string '%s'", str)
	}
}

func TestCreateJS(t *testing.T) {
	ctx := NewContext()

	val, err := ctx.CreateJS("{a:1, b:{c:3}}", NO_FILE)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"a":1,"b":{"c":3}}`
	if val.ToJSON() != expected {
		t.Fatalf("Expected '%s', got '%s'", expected, val.ToJSON())
	}
}

type Example struct {
	One int
	Two string
}

func TestToValue(t *testing.T) {
	ctx := NewContext()

	val, err := ctx.ToValue(Example{1, "two"})
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"One":1,"Two":"two"}`
	if val.ToJSON() != expected {
		t.Fatalf("Expected '%s', got '%s'", expected, val.ToJSON())
	}
}

func TestToValueFunc(t *testing.T) {
	ctx := NewContext()

	called := false
	f, err := ctx.ToValue(func(_ Loc, args ...*Value) (*Value, error) {
		called = true
		return ctx.ToValue(17)
	})
	if err != nil {
		t.Fatal(err)
	}

	val, err := ctx.Apply(f, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("Function was never called")
	}
	expected := "17"
	if val.ToJSON() != expected {
		t.Fatalf("Expected '%s', got '%s'", expected, val.ToJSON())
	}
}

func TestBurst(t *testing.T) {
	ctx := NewContext()

	ob, err := ctx.CreateJS("{a:1, b:{c:3}}", NO_FILE)
	if err != nil {
		t.Fatal(err)
	}

	vals, err := ob.Burst()
	if err != nil {
		t.Fatal(err)
	}

	if len(vals) != 2 {
		t.Fatal("Expected 2 vals, got ", len(vals), ":", vals)
	}

	if a, ok := vals["a"]; !ok {
		t.Fatal("Vals missing field 'a': ", vals)
	} else if a.ToJSON() != "1" {
		t.Fatal("Expected a to be 1, got ", a.ToJSON())
	}

	if b, ok := vals["b"]; !ok {
		t.Fatal("Vals missing field 'b': ", vals)
	} else if b.ToJSON() != `{"c":3}` {
		t.Fatal(`Expected b to be '{"c":3}', got `, b.ToJSON())
	}
}

func TestBurstOnNonObjectError(t *testing.T) {
	ctx := NewContext()

	num, err := ctx.CreateJS("5", NO_FILE)
	if err != nil {
		t.Fatal(err)
	}

	vals, err := num.Burst()
	if err == nil {
		t.Fatalf("Expected an error, got %v", vals)
	}
}

func TestGetObjectField(t *testing.T) {
	ctx := NewContext()

	ob, err := ctx.CreateJS("{a:1}", NO_FILE)
	if err != nil {
		t.Fatal(err)
	}

	if a, err := ob.Get("a"); err != nil {
		t.Fatal("Failed getting value: ", err)
	} else {
		if a.ToJSON() != "1" {
			t.Fatal("Expected '1', got ", a)
		}
	}
}

func TestGetObjectNonexistentField(t *testing.T) {
	ctx := NewContext()

	ob, err := ctx.CreateJS("{}", NO_FILE)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ob.Get("a"); err == nil {
		t.Fatal("Expected an error but did not get one.")
	}
}

func TestSetObjectField(t *testing.T) {
	ctx := NewContext()

	// Create an object.
	ob, err := ctx.CreateJS("{}", NO_FILE)
	if err != nil {
		t.Fatal(err)
	}

	// Create a simple number.
	num, err := ctx.CreateJS("3", NO_FILE)
	if err != nil {
		t.Fatal(err)
	}

	// See if we can set fields on the object.
	if err := ob.Set("foo", num); err != nil {
		t.Fatal(err)
	}

	// Make sure the object actually holds those fields:
	if ob.ToJSON() != `{"foo":3}` {
		t.Errorf("Object should have foo field, but is: [%v]", ob.ToJSON())
	}

	// Make sure we can't set fields on a number:
	if err := num.Set("bar", ob); err == nil {
		t.Error("We're not supposed to be able to set a field on a number!")
	}

	// Hey, let's also create a function and put that on the object.
	called := false
	fn, err := ctx.CreateRawFunc(func(_ Loc, args ...*Value) (*Value, error) {
		called = true
		return num, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Assign the function to a field on the object.
	if err := ob.Set("func", fn); err != nil {
		t.Fatal(err)
	}

	// Ok, now how do we verify that it works?  We need to get a hold of the
	// object in JS and then call the function.
	ctx.AddRawFunc("getObject", func(_ Loc, args ...*Value) (*Value, error) { return ob, nil })
	if res, err := ctx.Eval("getObject().func()", "test"); err != nil {
		t.Fatal(err)
	} else if !called {
		t.Error("Function was not called.")
	} else if res.(float64) != 3.0 {
		t.Fatalf("Got [%#v] instead of 3.0", res)
	}
}

func TestFromJsonWithSingleQuotes(t *testing.T) {
	ctx := NewContext()

	actual_key := `a'x<\>"`
	json_escaped_key := `a'x<\\>\"`
	s := `{"` + json_escaped_key + `":3}`

	v, err := ctx.FromJSON(s)
	if err != nil {
		t.Error("Can't parse key: ", s, "\n", template.JSEscapeString(s))
		t.Fatal(err)
	}

	m, err := v.Burst()
	if err != nil {
		t.Fatal(err)
	}

	if _, exists := m[actual_key]; !exists {
		t.Fatalf("Failed to find key [%s] in map: %v", actual_key, m)
	}
}

func TestValueAcrossContextsFails(t *testing.T) {
	// This test verifies that Values CANNOT be used across contexts.
	// The expectation is that attempts to do so will panic.

	// Create a value in ctx1 and try to use that value in ctx2.  The value
	// is injected into ctx2 via the getVal() RawFunc.
	// NOTE: The contexts are created in disposable isolates, because
	// right now panicing in go code screws up some internal state of
	// V8.
	ctx1, ctx2 := NewContextInIsolate(NewIsolate()),
		NewContextInIsolate(NewIsolate())

	// Create the value.  Note that if we use ctx2 here (so that the values
	// are always from the same context), this test works fine.
	value_from_ctx1, err := ctx1.EvalRaw(`x={"s":4};x`, NO_FILE)
	if err != nil {
		t.Fatal(err)
	}

	ctx2.AddRawFunc("getVal", func(Loc, ...*Value) (*Value, error) {
		return value_from_ctx1, nil
	})

	// Catch the panic:
	defer func() {
		if err := recover(); err != nil {
			errmsg := err.(error).Error()
			if !strings.HasPrefix(errmsg, "Error processing return value") {
				t.Fatal("Unexpected panic message:", err)
			}
		}
	}()

	val, err := ctx2.EvalRaw("getVal()", NO_FILE) // expect panic() here.
	if err != nil {
		t.Fatal(err)
	}

	t.Fatal("Should never reach here unless using values across " +
		"contexts somehow magically works.")

	// In the future, if this actually works, here's how we'd verify it:
	m, err := val.Burst()
	if err != nil {
		t.Fatal(err)
	}

	if _, found := m["s"]; !found {
		t.Fatal("Missing expected map key:", m)
	}

	if val.ToJSON() != `{"s":4}` {
		t.Error("Value does not work across contexts.")
	}
}

func TestCreateRawFunc(t *testing.T) {
	ctx := NewContext()
	calls := 0
	f, err := ctx.CreateRawFunc(func(src Loc, args ...*Value) (*Value, error) {
		if src.Filename != "" {
			t.Errorf("Expected filename to be blank when using .Apply, but got %q", src.Filename)
		}
		calls++
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx.Apply(f, f)
	if calls != 1 {
		t.Errorf("Expected calls to be 1, got %d", calls)
	}
}

func TestThrow(t *testing.T) {
	ctx := NewContext()
	ctx.AddRawFunc("die", func(_ Loc, args ...*Value) (*Value, error) {
		return nil, fmt.Errorf("bart")
	})
	_, err := ctx.EvalRaw("die()", NO_FILE)
	if err == nil {
		t.Error("Expected an error.")
	} else if !strings.Contains(err.Error(), "bart") {
		t.Error("Error did not contain expected error message: ", err)
	}

	// After an exception, we should still be able to use the context:
	res, err := ctx.Eval(`1+2`, NO_FILE)
	if err != nil {
		t.Error("Unexpected error: ", err)
	} else if res.(float64) != 3 {
		t.Error("Wrong result: ", res)
	}
}
