#include "v8wrap.h"

#include "libplatform/libplatform.h"
#include "v8.h"
#include "v8context.h"
#include "v8isolate.h"

extern "C" PlatformPtr v8_init() {
  v8::Platform *platform = v8::platform::CreateDefaultPlatform();
  v8::V8::InitializePlatform(platform);
  v8::V8::Initialize();
  return (void *)platform;
}

extern "C" IsolatePtr v8_create_isolate() {
  return static_cast<IsolatePtr>(new V8Isolate());
}

extern "C" IsolatePtr v8_create_isolate_with_snapshot(SnapshotPtr snapshot) {
  return static_cast<IsolatePtr>(new V8Isolate(static_cast<v8::StartupData *>(snapshot)));
}

extern "C" void v8_release_isolate(IsolatePtr isolate) {
  delete static_cast<V8Isolate *>(isolate);
}

extern "C" SnapshotPtr v8_create_snapshot(const char *snapshot_js) {
  v8::StartupData startup_data = v8::V8::CreateSnapshotDataBlob(snapshot_js);
  if (startup_data.data == NULL)
    return NULL;
  return static_cast<SnapshotPtr>(new v8::StartupData(startup_data));
}

extern "C" void v8_release_snapshot(SnapshotPtr snapshot) {
  v8::StartupData *snapshot_ptr = static_cast<v8::StartupData *>(snapshot);
  delete[] snapshot_ptr->data;
  delete snapshot_ptr;
}

extern "C" ContextPtr v8_create_context(IsolatePtr isolate) {
  return static_cast<ContextPtr>(
      static_cast<V8Isolate *>(isolate)->MakeContext());
}

extern "C" void v8_release_context(ContextPtr ctx) {
  delete static_cast<V8Context *>(ctx);
}

extern "C" char *v8_execute(ContextPtr ctx, char *str, char *debugFilename) {
  return (static_cast<V8Context *>(ctx))->Execute(str, debugFilename);
}

extern "C" PersistentValuePtr v8_eval(ContextPtr ctx, char *str,
                                      char *debugFilename) {
  return (static_cast<V8Context *>(ctx))->Eval(str, debugFilename);
}

extern "C" PersistentValuePtr v8_apply(ContextPtr ctx, PersistentValuePtr func,
                                       PersistentValuePtr self, int argc,
                                       PersistentValuePtr *argv) {
  return (static_cast<V8Context *>(ctx))->Apply(func, self, argc, argv);
}

extern "C" char *PersistentToJSON(ContextPtr ctx,
                                  PersistentValuePtr persistent) {
  return (static_cast<V8Context *>(ctx))->PersistentToJSON(persistent);
}

extern "C" void *v8_BurstPersistent(ContextPtr ctx,
                                    PersistentValuePtr persistent,
                                    int *out_numKeys) {
  return (void *)((static_cast<V8Context *>(ctx))
                      ->BurstPersistent(persistent, out_numKeys));
}

extern "C" const char *v8_setPersistentField(ContextPtr ctx,
                                             PersistentValuePtr persistent,
                                             const char *field,
                                             PersistentValuePtr value) {
  return ((static_cast<V8Context *>(ctx))
              ->SetPersistentField(persistent, field, value));
}

extern "C" void v8_release_persistent(ContextPtr ctx,
                                      PersistentValuePtr persistent) {
  (static_cast<V8Context *>(ctx))->ReleasePersistent(persistent);
}

extern "C" char *v8_error(ContextPtr ctx) {
  return (static_cast<V8Context *>(ctx))->Error();
}

extern "C" void v8_throw(ContextPtr ctx, char *errmsg) {
  return (static_cast<V8Context *>(ctx))->Throw(errmsg);
}

extern "C" void v8_terminate(IsolatePtr isolate) {
  (static_cast<V8Isolate *>(isolate))->Terminate();
}
