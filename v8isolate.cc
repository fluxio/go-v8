#include "v8isolate.h"

#include <cstdlib>
#include <cstring>

void* ArrayBufferAllocator::Allocate(size_t length) {
  void* data = AllocateUninitialized(length);
  return data == nullptr ? data : memset(data, 0, length);
}

void* ArrayBufferAllocator::AllocateUninitialized(size_t length) {
  return malloc(length);
}

void ArrayBufferAllocator::Free(void* data, size_t) { free(data); }

V8Isolate::V8Isolate() {
  v8::Isolate::CreateParams create_params;
  create_params.array_buffer_allocator = &allocator;
  isolate_ = v8::Isolate::New(create_params);
}

V8Isolate::V8Isolate(v8::StartupData* startup_data) {
  v8::Isolate::CreateParams create_params;
  create_params.array_buffer_allocator = &allocator;
  create_params.snapshot_blob = startup_data;
  isolate_ = v8::Isolate::New(create_params);
}

V8Context* V8Isolate::MakeContext() { return new V8Context(isolate_); }

V8Isolate::~V8Isolate() { isolate_->Dispose(); }

void V8Isolate::Terminate() { v8::V8::TerminateExecution(isolate_); }
