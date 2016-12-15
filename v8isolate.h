#ifndef V8ISOLATE_H
#define V8ISOLATE_H

#include "v8.h"
#include "v8context.h"
#include "v8wrap.h"

class ArrayBufferAllocator : public v8::ArrayBuffer::Allocator {
 public:
  virtual void* Allocate(size_t length);
  virtual void* AllocateUninitialized(size_t length);
  virtual void Free(void* data, size_t);
};

class V8Isolate {
 public:
  V8Isolate();
  V8Isolate(v8::StartupData* startup_data);
  ~V8Isolate();

  V8Context* MakeContext();

  // May be called any any time, will forcefully terminate the VM.
  void Terminate();

 private:
  v8::Isolate* isolate_;
  ArrayBufferAllocator allocator;
};

#endif  // !defined(V8ISOLATE_H)
