// Package v8 provides a Go API for interacting with the V8 javascript library.
package v8

// #include <stdlib.h>
// #include "v8wrap.h"
// #cgo CXXFLAGS: -std=c++11
// #cgo LDFLAGS: -lv8_base -lv8_libbase -lv8_snapshot -lv8_libplatform -ldl -pthread
import "C"

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"runtime"
	"sync"
	"text/template"
	"unsafe"
)

var contexts = make(map[uint]*V8Context)
var contextsMutex sync.RWMutex
var highestContextId uint

// A constant indicating that a particular script evaluation is not associated
// with any file.
const NO_FILE = ""

// Value represents a handle to a V8::Value.  It is associated with a particular
// context and attempts to use it with a different context will fail.
type Value struct {
	ptr C.PersistentValuePtr
	ctx *V8Context
}

// ToJSON converts the value to a JSON string.
func (v *Value) ToJSON() string {
	if v.ctx == nil || v.ptr == nil {
		panic("Value or context were reset.")
	}
	str := C.PersistentToJSON(v.ctx.v8context, v.ptr)
	defer C.free(unsafe.Pointer(str))
	return C.GoString(str)
}

// ToString converts a value holding a JS String to a string.  If the value
// is not actually a string, this will return an error.
func (v *Value) ToString() (string, error) {
	if v.ctx == nil || v.ptr == nil {
		panic("Value or context were reset.")
	}
	var str string
	err := json.Unmarshal([]byte(v.ToJSON()), &str)
	return str, err
}

// Burst converts a value that represents a JS Object and returns a map of
// key -> Value for each of the object's fields.  If the value is not an
// Object, an error is returned.
func (v *Value) Burst() (map[string]*Value, error) {
	if v.ctx == nil || v.ptr == nil {
		panic("Value or context were reset.")
	}
	// Call cgo to burst the object, get a list of KeyValuePairs back.
	var numKeys C.int
	keyValuesPtr := C.v8_BurstPersistent(v.ctx.v8context, v.ptr, &numKeys)

	if keyValuesPtr == nil {
		err := C.v8_error(v.ctx.v8context)
		defer C.free(unsafe.Pointer(err))
		return nil, errors.New(C.GoString(err) + ":" + v.ToJSON())
	}

	// Convert the list to a slice:
	var keyValues []C.struct_KeyValuePair
	sliceHeader := (*reflect.SliceHeader)((unsafe.Pointer(&keyValues)))
	sliceHeader.Cap = int(numKeys)
	sliceHeader.Len = int(numKeys)
	sliceHeader.Data = uintptr(unsafe.Pointer(keyValuesPtr))

	// Create the object map:
	result := make(map[string]*Value)
	for _, keyVal := range keyValues {
		key := C.GoString(keyVal.keyName)
		val := v.ctx.newValue(keyVal.value)

		result[key] = val

		// Don't forget to clean up!
		C.free(unsafe.Pointer(keyVal.keyName))
	}
	return result, nil
}

// Returns the given field of the object.
// TODO(mag): optimize.
func (v *Value) Get(field string) (*Value, error) {
	if v == nil {
		panic("nil value")
	}
	if v.ctx == nil || v.ptr == nil {
		panic("Value or context were reset.")
	}

	fields, err := v.Burst()
	if err != nil {
		return nil, err
	}
	res, exists := fields[field]
	if !exists || res == nil {
		return nil, fmt.Errorf("field '%s' is undefined.", field)
	}
	return res, nil
}

func (v *Value) Set(field string, val *Value) error {
	if v.ctx == nil || v.ptr == nil {
		panic("Value or context were reset.")
	}
	fieldPtr := C.CString(field)
	defer C.free(unsafe.Pointer(fieldPtr))
	errmsg := C.v8_setPersistentField(v.ctx.v8context, v.ptr, fieldPtr, val.ptr)
	if errmsg != nil {
		return errors.New(C.GoString(errmsg))
	}
	return nil
}

//export _go_v8_callback
func _go_v8_callback(ctxID uint, name, args *C.char) *C.char {
	runtime.UnlockOSThread()
	defer runtime.LockOSThread()

	contextsMutex.RLock()
	c := contexts[ctxID]
	contextsMutex.RUnlock()
	f := c.funcs[C.GoString(name)]
	if f != nil {
		var argv []interface{}
		json.Unmarshal([]byte(C.GoString(args)), &argv)
		ret := f(argv...)
		if ret != nil {
			b, _ := json.Marshal(ret)
			return C.CString(string(b))
		}
		return nil
	}
	return C.CString("undefined")
}

// TODO(mag): catch all panics in go functions, that are called from C code.
//export _go_v8_callback_raw
func _go_v8_callback_raw(
	ctxID uint,
	name *C.char,
	callerFuncName, callerScriptName *C.char,
	callerLineNumber, callerColumn C.int,
	argc C.int,
	argvptr C.PersistentValuePtr,
) C.PersistentValuePtr {
	runtime.UnlockOSThread()
	defer runtime.LockOSThread()

	funcname := C.GoString(name)

	caller := Loc{
		Funcname: C.GoString(callerFuncName),
		Filename: C.GoString(callerScriptName),
		Line:     int(callerLineNumber),
		Column:   int(callerColumn),
	}

	contextsMutex.RLock()
	ctx := contexts[ctxID]
	contextsMutex.RUnlock()
	function := ctx.rawFuncs[funcname]

	var argv []C.PersistentValuePtr
	sliceHeader := (*reflect.SliceHeader)((unsafe.Pointer(&argv)))
	sliceHeader.Cap = int(argc)
	sliceHeader.Len = int(argc)
	sliceHeader.Data = uintptr(unsafe.Pointer(argvptr))

	if function == nil {
		panic(fmt.Errorf("No such registered raw function: %s", C.GoString(name)))
	}

	args := make([]*Value, argc)
	for i := 0; i < int(argc); i++ {
		args[i] = ctx.newValue(argv[i])
	}

	res, err := function(caller, args...)

	if err != nil {
		ctx.throw(err)
		return nil
	}

	if res == nil {
		return nil
	}

	if res.ctx.v8context != ctx.v8context {
		panic(fmt.Errorf("Error processing return value of raw function callback %s: "+
			"Return value was generated from another context.", C.GoString(name)))
	}

	return res.ptr
}

// Function is the callback signature for functions that are registered with
// a V8 context.  Arguments and return values are POD serialized via JSON and
// unmarshaled into Go interface{}s via json.Unmarshal.
type Function func(...interface{}) interface{}

// Loc defines a script location.
type Loc struct {
	Funcname, Filename string
	Line, Column       int
}

// RawFunction is the callback signature for functions that are registered with
// a V8 context via AddRawFunc().  The first argument is the script location
// that is calling the regsitered RawFunction, and remaining arguments and
// return values are Value objects that represent handles to data within the V8
// context.  If the function is called directly from Go (e.g. via Apply()), then
// "from" will be empty. Never return a Value from a different V8 context.
type RawFunction func(from Loc, args ...*Value) (*Value, error)

type V8Isolate struct {
	v8isolate C.IsolatePtr
}

// V8Context is a handle to a v8 context.
type V8Context struct {
	id        uint
	v8context C.ContextPtr
	v8isolate *V8Isolate
	funcs     map[string]Function
	rawFuncs  map[string]RawFunction
	values    map[*Value]bool
	valuesMu  *sync.Mutex
}

var platform C.PlatformPtr
var defaultIsolate *V8Isolate

func init() {
	platform = C.v8_init()
	defaultIsolate = NewIsolate()
}
func NewIsolate() *V8Isolate {
	res := &V8Isolate{C.v8_create_isolate()}
	runtime.SetFinalizer(res, func(i *V8Isolate) {
		C.v8_release_isolate(i.v8isolate)
	})
	return res
}

// NewContext creates a V8 context in a default isolate
// and returns a handle to it.
func NewContext() *V8Context {
	return NewContextInIsolate(defaultIsolate)
}

// NewContext creates a V8 context in a given isolate
// and returns a handle to it.
func NewContextInIsolate(isolate *V8Isolate) *V8Context {
	v := &V8Context{
		v8context: C.v8_create_context(isolate.v8isolate),
		v8isolate: isolate,
		funcs:     make(map[string]Function),
		rawFuncs:  make(map[string]RawFunction),
		values:    make(map[*Value]bool),
		valuesMu:  &sync.Mutex{},
	}

	contextsMutex.Lock()
	highestContextId += 1
	v.id = highestContextId
	contexts[v.id] = v
	contextsMutex.Unlock()

	runtime.SetFinalizer(v, func(p *V8Context) {
		p.Destroy()
	})
	return v
}

// Releases the context handle and all the values allocated within the context.
// NOTE: The context can't be used for anything after this function is called.
func (v *V8Context) Destroy() error {
	if v.v8context == nil {
		return errors.New("Context is uninitialized.")
	}
	v.ClearValues()

	contextsMutex.Lock()
	delete(contexts, v.id)
	contextsMutex.Unlock()

	C.v8_release_context(v.v8context)
	v.v8context = nil
	v.v8isolate = nil
	return nil
}

// Releases all the values allocated in this context.
func (v *V8Context) ClearValues() error {
	if v.v8context == nil {
		panic("Context is uninitialized.")
	}
	v.valuesMu.Lock()
	for val, _ := range v.values {
		v.releaseValueLocked(val)
	}
	v.valuesMu.Unlock()
	return nil
}

// Releases the v8 hanle that val points to.
// NOTE: The val object can't be used after this function is called on it.
func (v *V8Context) ReleaseValue(val *Value) error {
	if v.v8context == nil {
		panic("Context is uninitialized.")
	}
	v.valuesMu.Lock()
	defer v.valuesMu.Unlock()
	if val.ptr == nil {
		return errors.New("Value has been already released.")
	}
	v.releaseValueLocked(val)
	return nil
}

// Dispose of the persistent object and free the allocated handle.
func (v *V8Context) releaseValueLocked(val *Value) {
	val.ctx = nil
	delete(v.values, val)
	C.v8_release_persistent(v.v8context, val.ptr)
}

func (v *V8Context) newValue(ptr C.PersistentValuePtr) *Value {
	res := &Value{ptr, v}
	v.valuesMu.Lock()
	v.values[res] = true
	v.valuesMu.Unlock()
	return res
}

// Stops the computation running inside the isolate.
func (iso *V8Isolate) Terminate() {
	C.v8_terminate(iso.v8isolate)
}

// Eval executes the provided javascript within the V8 context.  The javascript
// is executed as if it was from the specified file, so that any errors or stack
// traces are annotated with the corresponding file/line number.
//
// The result of the javascript is returned as POD serialized via JSON and
// unmarshaled back into Go, otherwise an error is returned.
func (v *V8Context) Eval(javascript string, filename string) (res interface{}, err error) {
	if v.v8context == nil {
		panic("Context is uninitialized.")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	jsPtr := C.CString(javascript)
	defer C.free(unsafe.Pointer(jsPtr))
	var filenamePtr *C.char
	if len(filename) > 0 {
		filenamePtr = C.CString(filename)
		defer C.free(unsafe.Pointer(filenamePtr))
	}
	ret := C.v8_execute(v.v8context, jsPtr, filenamePtr)
	if ret != nil {
		out := C.GoString(ret)
		if out != "" {
			C.free(unsafe.Pointer(ret))
			err := json.Unmarshal([]byte(out), &res)
			return res, err
		}
		return out, nil
	}
	ret = C.v8_error(v.v8context)
	out := C.GoString(ret)
	C.free(unsafe.Pointer(ret))
	return nil, errors.New(out)
}

func (v *V8Context) convertToValue(e error) *Value {
	strdata, err := json.Marshal(e.Error())
	if err != nil {
		panic(err)
	}

	val, err := v.FromJSON(string(strdata))
	if err != nil {
		panic(err)
	}
	return val
}

func (v *V8Context) throw(err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	msg := C.CString(err.Error())
	defer C.free(unsafe.Pointer(msg))
	C.v8_throw(v.v8context, msg)
}

// Call the named function within the v8 context with the specified parameters.
// Parameters are serialized via JSON.
func (v *V8Context) Run(funcname string, args ...interface{}) (interface{}, error) {
	if v.v8context == nil {
		panic("Context is uninitialized.")
	}

	var cmd bytes.Buffer

	fmt.Fprint(&cmd, funcname, "(")
	for i, arg := range args {
		if i > 0 {
			fmt.Fprint(&cmd, ",")
		}
		err := json.NewEncoder(&cmd).Encode(arg)
		if err != nil {
			return nil, err
		}
	}
	fmt.Fprint(&cmd, ")")
	return v.Eval(cmd.String(), fmt.Sprintf("[RUN:%v]", funcname))
}

// FromJSON parses a JSON string and returns a Value that references the parsed
// data in the V8 context.
func (v *V8Context) FromJSON(s string) (*Value, error) {
	if v.v8context == nil {
		panic("Context is uninitialized.")
	}
	return v.EvalRaw("JSON.parse('"+template.JSEscapeString(s)+"')", "FromJSON")
}

// CreateJS evalutes the specified javascript object and returns a handle to the
// result.  This allows:
//   (1) Creating objects using JS notation rather than JSON notation:
//        val = ctx.CreateJS("{a:1}", NO_FILE)
//   (2) Creating function values:
//        val = ctx.CreateJS("function(a,b) { return a+b; }", NO_FILE)
//   (3) Creating objects that reference existing state in the context:
//        val = ctx.CreateJS("{a:some_func, b:console.log}", NO_FILE)
// The filename parameter may be specified to provide additional debugging in
// the case of failures.
func (v *V8Context) CreateJS(js, filename string) (*Value, error) {
	if v.v8context == nil {
		panic("Context is uninitialized.")
	}
	script := fmt.Sprintf(`(function() { return %s; })()`, js)
	return v.EvalRaw(script, filename)
}

// EvalRaw executes the provided javascript within the V8 context.  The
// javascript is executed as if it was from the specified file, so that any
// errors or stack traces are annotated with the corresponding file/line number.
//
// The result of the javascript is returned as a handle to the result in the V8
// engine if it succeeded, otherwise an error is returned.  Unlike Eval, this
// does not do any JSON marshalling/unmarshalling of the results
func (ctx *V8Context) EvalRaw(js string, filename string) (*Value, error) {
	if ctx.v8context == nil {
		panic("Context is uninitialized.")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	jsPtr := C.CString(js)
	defer C.free(unsafe.Pointer(jsPtr))

	filenamePtr := C.CString(filename)
	defer C.free(unsafe.Pointer(filenamePtr))

	ret := C.v8_eval(ctx.v8context, jsPtr, filenamePtr)
	if ret == nil {
		err := C.v8_error(ctx.v8context)
		defer C.free(unsafe.Pointer(err))
		return nil, fmt.Errorf("Failed to execute JS (%s): %s", filename, C.GoString(err))
	}

	val := ctx.newValue(ret)

	return val, nil
}

// Apply will execute a JS Function with the specified 'this' context and
// parameters. If 'this' is nil, then the function is executed in the global
// scope.  f must be a Value handle that holds a JS function.  Other
// parameters may be any Value.
func (ctx *V8Context) Apply(f, this *Value, args ...*Value) (*Value, error) {
	if ctx.v8context == nil {
		panic("Context is uninitialized.")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// always allocate at least one so &argPtrs[0] works.
	argPtrs := make([]C.PersistentValuePtr, len(args)+1)
	for i := range args {
		argPtrs[i] = args[i].ptr
	}
	var thisPtr C.PersistentValuePtr
	if this != nil {
		thisPtr = this.ptr
	}
	ret := C.v8_apply(ctx.v8context, f.ptr, thisPtr, C.int(len(args)), &argPtrs[0])
	if ret == nil {
		err := C.v8_error(ctx.v8context)
		defer C.free(unsafe.Pointer(err))
		return nil, errors.New(C.GoString(err))
	}

	val := ctx.newValue(ret)

	return val, nil
}

// Terminate forcibly stops execution of a V8 context.  This can be safely run
// from any goroutine or thread.  If the V8 context is not running anything,
// this will have no effect.
func (v *V8Context) Terminate() {
	v.v8isolate.Terminate()
}

// AddFunc adds a function into the V8 context.
func (v *V8Context) AddFunc(name string, f Function) error {
	v.funcs[name] = f
	jsCall := fmt.Sprintf(`function %v() {
		  return _go_call(%v, "%v", JSON.stringify([].slice.call(arguments)));
		}`, name, v.id, name)
	funcname, filepath, line := funcInfo(f)
	_, err := v.Eval(jsCall, fmt.Sprintf("native callback to %s [%s:%d]",
		path.Ext(funcname)[1:], path.Base(filepath), line))
	return err
}

// AddRawFunc adds a raw function into the V8 context.
func (v *V8Context) AddRawFunc(name string, f RawFunction) error {
	v.rawFuncs[name] = f
	jsCall := fmt.Sprintf(`function %v() {
			return _go_call_raw(%v, "%v", [].slice.call(arguments));
		}`, name, v.id, name)
	funcname, filepath, line := funcInfo(f)
	_, err := v.Eval(jsCall, fmt.Sprintf("native callback to %s [%s:%d]",
		path.Ext(funcname)[1:], path.Base(filepath), line))
	return err
}

// CreateRawFunc adds a raw function into the V8 context without polluting the
// namespace.  The only reference to the function is returned as a *v8.Value.
func (v *V8Context) CreateRawFunc(f RawFunction) (fn *Value, err error) {
	funcname, filepath, line := funcInfo(f)
	name := fmt.Sprintf("RawFunc:%s@%s:%d", funcname, path.Base(filepath), line)
	name = template.JSEscapeString(name)
	v.rawFuncs[name] = f
	jscode := fmt.Sprintf(`(function() {
		return _go_call_raw(%v, "%v", [].slice.call(arguments));
	})`, v.id, name)
	return v.EvalRaw(jscode, name)
}

// Attempts to convert a native Go value into a *Value.  If the native
// value is a RawFunction, it will create a function Value.  Otherwise, the
// value must be JSON serializable and the corresponding JS object will be
// constructed.
// NOTE: If the original object has several references to the same object,
// in the resulting JS object those references will become independent objects.
func (v *V8Context) ToValue(val interface{}) (*Value, error) {
	if fn, isFunction := val.(func(Loc, ...*Value) (*Value, error)); isFunction {
		return v.CreateRawFunc(fn)
	}
	data, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("Cannot marshal value as JSON: %v\nVal: %#v", err, val)
	}
	return v.FromJSON(string(data))
}

// Given any function, it will return the name (a/b/pkg.name), full filename
// path, and line number of that function.
func funcInfo(function interface{}) (name, filepath string, line int) {
	ptr := reflect.ValueOf(function)
	f := runtime.FuncForPC(ptr.Pointer())
	file, line := f.FileLine(f.Entry())
	return f.Name(), file, line
}
