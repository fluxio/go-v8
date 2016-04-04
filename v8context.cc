#include "v8context.h"

#include <cstdlib>
#include <cstring>
#include <sstream>

extern "C" char* _go_v8_callback(unsigned int ctxID, char* name, char* args);

extern "C" PersistentValuePtr _go_v8_callback_raw(
    unsigned int ctxID, const char* name, const char* callerFuncname,
    const char* callerFilename, int callerLine, int callerColumn, int argc,
    PersistentValuePtr* argv);

namespace {

// Calling JSON.stringify on value.
std::string to_json(v8::Isolate* iso, v8::Local<v8::Value> value) {
  v8::HandleScope scope(iso);
  v8::TryCatch try_catch;
  v8::Local<v8::Object> json =
      v8::Local<v8::Object>::Cast(iso->GetCurrentContext()->Global()->Get(
          v8::String::NewFromUtf8(iso, "JSON")));
  v8::Local<v8::Function> func = v8::Local<v8::Function>::Cast(
      json->GetRealNamedProperty(v8::String::NewFromUtf8(iso, "stringify")));
  v8::Local<v8::Value> args[1];
  args[0] = value;
  v8::String::Utf8Value ret(
      func->Call(iso->GetCurrentContext()->Global(), 1, args)->ToString());
  return *ret;
}

// Calling JSON.parse on str.
v8::Local<v8::Value> from_json(v8::Isolate* iso, std::string str) {
  v8::HandleScope scope(iso);
  v8::TryCatch try_catch;
  v8::Local<v8::Object> json =
      v8::Local<v8::Object>::Cast(iso->GetCurrentContext()->Global()->Get(
          v8::String::NewFromUtf8(iso, "JSON")));
  v8::Local<v8::Function> func = v8::Local<v8::Function>::Cast(
      json->GetRealNamedProperty(v8::String::NewFromUtf8(iso, "parse")));
  v8::Local<v8::Value> args[1];
  args[0] = v8::String::NewFromUtf8(iso, str.c_str());
  return func->Call(iso->GetCurrentContext()->Global(), 1, args);
}

// _go_call is a helper function to call Go functions from within v8.
void _go_call(const v8::FunctionCallbackInfo<v8::Value>& args) {
  uint32_t id = args[0]->ToUint32()->Value();
  v8::String::Utf8Value name(args[1]);
  v8::String::Utf8Value argv(args[2]);
  v8::Isolate* iso = args.GetIsolate();
  v8::HandleScope scope(iso);
  v8::ReturnValue<v8::Value> ret = args.GetReturnValue();
  char* retv = _go_v8_callback(id, *name, *argv);
  if (retv != NULL) {
    ret.Set(from_json(iso, retv));
    free(retv);
  }
}

std::string str(v8::Local<v8::Value> value) {
  v8::String::Utf8Value s(value);
  if (s.length() == 0) {
    return "";
  }
  return *s;
}

// _go_call_raw is a helper function to call Go functions from within v8.
void _go_call_raw(const v8::FunctionCallbackInfo<v8::Value>& args) {
  v8::Isolate* iso = args.GetIsolate();
  v8::HandleScope scope(iso);

  uint32_t id = args[0]->ToUint32()->Value();
  v8::String::Utf8Value name(args[1]);
  v8::Local<v8::Array> hargv = v8::Local<v8::Array>::Cast(args[2]);

  std::string src_file, src_func;
  int line_number = 0, column = 0;
  v8::Local<v8::StackTrace> trace(v8::StackTrace::CurrentStackTrace(iso, 2));
  if (trace->GetFrameCount() == 2) {
    v8::Local<v8::StackFrame> frame(trace->GetFrame(1));
    src_file = str(frame->GetScriptName());
    src_func = str(frame->GetFunctionName());
    line_number = frame->GetLineNumber();
    column = frame->GetColumn();
  }

  int argc = hargv->Length();
  PersistentValuePtr argv[argc];
  for (int i = 0; i < argc; i++) {
    argv[i] = new v8::Persistent<v8::Value>(iso, hargv->Get(i));
  }

  PersistentValuePtr retv =
      _go_v8_callback_raw(id, *name, src_func.c_str(), src_file.c_str(),
                          line_number, column, argc, argv);

  if (retv == NULL) {
    args.GetReturnValue().Set(v8::Undefined(iso));
  } else {
    args.GetReturnValue().Set(*static_cast<v8::Persistent<v8::Value>*>(retv));
  }
}
};

V8Context::V8Context(v8::Isolate* isolate) : mIsolate(isolate) {
  v8::Locker lock(mIsolate);
  v8::Isolate::Scope isolate_scope(mIsolate);
  v8::HandleScope handle_scope(mIsolate);

  v8::V8::SetCaptureStackTraceForUncaughtExceptions(true);

  v8::Local<v8::ObjectTemplate> globals = v8::ObjectTemplate::New(mIsolate);
  globals->Set(v8::String::NewFromUtf8(mIsolate, "_go_call"),
               v8::FunctionTemplate::New(mIsolate, _go_call));
  globals->Set(v8::String::NewFromUtf8(mIsolate, "_go_call_raw"),
               v8::FunctionTemplate::New(mIsolate, _go_call_raw));

  mContext.Reset(mIsolate, v8::Context::New(mIsolate, NULL, globals));
};

V8Context::~V8Context() {
  v8::Locker lock(mIsolate);
  mContext.Reset();
};

char* V8Context::Execute(const char* source, const char* filename) {
  v8::Locker locker(mIsolate);
  v8::Isolate::Scope isolate_scope(mIsolate);
  v8::HandleScope handle_scope(mIsolate);
  v8::Context::Scope context_scope(mContext.Get(mIsolate));
  v8::TryCatch try_catch;
  try_catch.SetVerbose(false);

  mLastError.clear();

  v8::Local<v8::Script> script = v8::Script::Compile(
      v8::String::NewFromUtf8(mIsolate, source),
      v8::String::NewFromUtf8(mIsolate, filename ? filename : "undefined"));

  if (script.IsEmpty()) {
    mLastError = report_exception(try_catch);
    return NULL;
  }

  v8::Local<v8::Value> result = script->Run();

  if (result.IsEmpty()) {
    mLastError = report_exception(try_catch);
    return NULL;
  }

  if (result->IsFunction() || result->IsUndefined()) {
    return strdup("");
  } else {
    return strdup(to_json(mIsolate, result).c_str());
  }
}

PersistentValuePtr V8Context::Eval(const char* source, const char* filename) {
  v8::Locker locker(mIsolate);
  v8::Isolate::Scope isolate_scope(mIsolate);
  v8::HandleScope handle_scope(mIsolate);
  v8::Context::Scope context_scope(mContext.Get(mIsolate));
  v8::TryCatch try_catch;
  try_catch.SetVerbose(false);

  mLastError.clear();

  v8::Local<v8::Script> script = v8::Script::Compile(
      v8::String::NewFromUtf8(mIsolate, source),
      filename ? v8::String::NewFromUtf8(mIsolate, filename)
               : v8::String::NewFromUtf8(mIsolate, "undefined"));

  if (script.IsEmpty()) {
    mLastError = report_exception(try_catch);
    return NULL;
  }

  v8::Local<v8::Value> result = script->Run();

  if (result.IsEmpty()) {
    mLastError = report_exception(try_catch);
    return NULL;
  }

  return new v8::Persistent<v8::Value>(mIsolate, result);
}

PersistentValuePtr V8Context::Apply(PersistentValuePtr func,
                                    PersistentValuePtr self, int argc,
                                    PersistentValuePtr* argv) {
  v8::Locker locker(mIsolate);
  v8::Isolate::Scope isolate_scope(mIsolate);
  v8::HandleScope handle_scope(mIsolate);
  v8::Context::Scope context_scope(mContext.Get(mIsolate));
  v8::TryCatch try_catch;
  try_catch.SetVerbose(false);

  mLastError.clear();

  v8::Local<v8::Value> pfunc =
      static_cast<v8::Persistent<v8::Value>*>(func)->Get(mIsolate);
  v8::Local<v8::Function> vfunc = v8::Local<v8::Function>::Cast(pfunc);

  v8::Local<v8::Value>* vargs = new v8::Local<v8::Value>[argc];
  for (int i = 0; i < argc; i++) {
    vargs[i] = static_cast<v8::Persistent<v8::Value>*>(argv[i])->Get(mIsolate);
  }

  // Global scope requested?
  v8::Local<v8::Object> vself;
  if (self == NULL) {
    vself = mContext.Get(mIsolate)->Global();
  } else {
    v8::Local<v8::Value> pself =
        static_cast<v8::Persistent<v8::Value>*>(self)->Get(mIsolate);
    vself = v8::Local<v8::Object>::Cast(pself);
  }

  v8::Local<v8::Value> result = vfunc->Call(vself, argc, vargs);

  delete[] vargs;

  if (result.IsEmpty()) {
    mLastError = report_exception(try_catch);
    return NULL;
  }

  return new v8::Persistent<v8::Value>(mIsolate, result);
}

char* V8Context::PersistentToJSON(PersistentValuePtr persistent) {
  v8::Locker locker(mIsolate);
  v8::Isolate::Scope isolate_scope(mIsolate);
  v8::HandleScope handle_scope(mIsolate);
  v8::Context::Scope context_scope(mContext.Get(mIsolate));
  v8::Local<v8::Value> persist =
      static_cast<v8::Persistent<v8::Value>*>(persistent)->Get(mIsolate);
  return strdup(to_json(mIsolate, persist).c_str());
}

void V8Context::ReleasePersistent(PersistentValuePtr persistent) {
  v8::Locker locker(mIsolate);
  v8::Persistent<v8::Value>* persist =
      static_cast<v8::Persistent<v8::Value>*>(persistent);
  persist->Reset();
  delete persist;
}

const char* V8Context::SetPersistentField(PersistentValuePtr persistent,
                                          const char* field,
                                          PersistentValuePtr value) {
  v8::Locker locker(mIsolate);
  v8::Isolate::Scope isolate_scope(mIsolate);
  v8::HandleScope handle_scope(mIsolate);
  v8::Context::Scope context_scope(mContext.Get(mIsolate));
  v8::Persistent<v8::Value>* persist =
      static_cast<v8::Persistent<v8::Value>*>(persistent);
  v8::Local<v8::Value> name(v8::String::NewFromUtf8(mIsolate, field));

  // Create the local object now, but reset the persistent one later:
  // we could still fail setting the value, and then there is no point
  // in re-creating the persistent copy.
  v8::Local<v8::Value> maybeObject = persist->Get(mIsolate);
  if (!maybeObject->IsObject()) {
    return "The supplied receiver is not an object.";
  }

  // We can safely call `ToLocalChecked`, because
  // we've just created the local object above.
  v8::Local<v8::Object> object =
      maybeObject->ToObject(mContext.Get(mIsolate)).ToLocalChecked();

  v8::Persistent<v8::Value>* val =
      static_cast<v8::Persistent<v8::Value>*>(value);
  v8::Local<v8::Value> local_val = val->Get(mIsolate);

  if (!object->Set(name, local_val)) {
    return "Cannot set value";
  }

  // Now it is time to get rid of the previous persistent version,
  // and create a new one.
  persist->Reset(mIsolate, object);

  return NULL;
}

KeyValuePair* V8Context::BurstPersistent(PersistentValuePtr persistent,
                                         int* out_numKeys) {
  v8::Locker locker(mIsolate);
  v8::Isolate::Scope isolate_scope(mIsolate);
  v8::HandleScope handle_scope(mIsolate);
  v8::Context::Scope context_scope(mContext.Get(mIsolate));
  v8::Persistent<v8::Value>* persist =
      static_cast<v8::Persistent<v8::Value>*>(persistent);

  mLastError.clear();

  v8::Local<v8::Value> maybeObject = persist->Get(mIsolate);

  if (!maybeObject->IsObject()) {
    return NULL;  // Triggers an error creation upstream.
  }

  // We can safely call `ToLocalChecked`, because
  // we've just created the local object above.
  v8::Local<v8::Object> object =
      maybeObject->ToObject(mContext.Get(mIsolate)).ToLocalChecked();
  v8::Local<v8::Array> keys(object->GetPropertyNames());
  int num_keys = keys->Length();
  *out_numKeys = num_keys;
  KeyValuePair* result = new KeyValuePair[num_keys];
  for (int i = 0; i < num_keys; i++) {
    v8::Local<v8::Value> key = keys->Get(i);
    result[i].keyName = strdup(str(key).c_str());
    result[i].value = new v8::Persistent<v8::Value>(mIsolate, object->Get(key));
  }

  return result;
}

std::string V8Context::report_exception(v8::TryCatch& try_catch) {
  std::stringstream ss;
  ss << "Uncaught exception: ";

  std::string exceptionStr = str(try_catch.Exception());
  if (exceptionStr == "[object Object]") {
    ss << to_json(mIsolate, try_catch.Exception());
  } else {
    ss << exceptionStr;
  }

  if (!try_catch.Message().IsEmpty()) {
    ss << std::endl
       << "at " << str(try_catch.Message()->GetScriptResourceName()) << ":"
       << try_catch.Message()->GetLineNumber() << ":"
       << try_catch.Message()->GetStartColumn() << ":"
       << str(try_catch.Message()->GetSourceLine());
  }

  if (!try_catch.StackTrace().IsEmpty()) {
    ss << std::endl << "Stack trace: " << str(try_catch.StackTrace());
  }

  return ss.str();
}

void V8Context::Throw(const char* errmsg) {
  v8::Locker locker(mIsolate);
  v8::Isolate::Scope isolate_scope(mIsolate);
  v8::HandleScope handle_scope(mIsolate);
  v8::Context::Scope context_scope(mContext.Get(mIsolate));
  v8::Local<v8::Value> err =
      v8::Exception::Error(v8::String::NewFromUtf8(mIsolate, errmsg));
  mIsolate->ThrowException(err);
}

char* V8Context::Error() {
  v8::Locker locker(mIsolate);
  return strdup(mLastError.c_str());
}
