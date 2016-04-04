#ifndef V8CONTEXT_H
#define V8CONTEXT_H

#include <string>

#include "v8.h"
#include "v8wrap.h"

class V8Context {
 public:
  V8Context(v8::Isolate* isolate);
  ~V8Context();

  char* Execute(const char* source, const char* filename);
  char* Error();

  PersistentValuePtr Eval(const char* str, const char* debugFilename);

  PersistentValuePtr Apply(PersistentValuePtr func, PersistentValuePtr self,
                           int argc, PersistentValuePtr* argv);

  char* PersistentToJSON(PersistentValuePtr persistent);

  void ReleasePersistent(PersistentValuePtr persistent);
  KeyValuePair* BurstPersistent(PersistentValuePtr persistent,
                                int* out_numKeys);

  // Returns an error message on failure, otherwise returns NULL.
  const char* SetPersistentField(PersistentValuePtr persistent,
                                 const char* field, PersistentValuePtr value);

  void Throw(const char* errmsg);

 private:
  std::string report_exception(v8::TryCatch& try_catch);

  v8::Isolate* mIsolate;
  v8::Persistent<v8::Context> mContext;
  std::string mLastError;
};

#endif  // !defined(V8CONTEXT_H)
