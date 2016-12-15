#ifndef V8WRAP_H
#define V8WRAP_H

#ifdef __cplusplus
extern "C" {
#endif

typedef void *IsolatePtr;
typedef void *ContextPtr;
typedef void *PersistentValuePtr;
typedef void *PlatformPtr;
typedef void *SnapshotPtr;

extern PlatformPtr v8_init();

extern IsolatePtr v8_create_isolate();

extern IsolatePtr v8_create_isolate_with_snapshot(SnapshotPtr snapshot);

extern void v8_release_isolate(IsolatePtr isolate);

extern SnapshotPtr v8_create_snapshot(const char *snapshot_js);

extern void v8_release_snapshot(SnapshotPtr snapshot);

extern ContextPtr v8_create_context(IsolatePtr isolate);

extern void v8_release_context(ContextPtr ctx);

extern char *v8_execute(ContextPtr ctx, char *str, char *debugFilename);

extern PersistentValuePtr v8_eval(ContextPtr ctx, char *str,
                                  char *debugFilename);

extern PersistentValuePtr v8_apply(ContextPtr ctx, PersistentValuePtr func,
                                   PersistentValuePtr self, int argc,
                                   PersistentValuePtr *argv);

extern char *PersistentToJSON(ContextPtr ctx, PersistentValuePtr persistent);

struct KeyValuePair {
  char *keyName;
  PersistentValuePtr value;
};

// Returns NULL on errors, otherwise allocates an array of KeyValuePairs
// and sets out_numkeys to the length.
// For some reason, cgo barfs if the return type is KeyValuePair*, so we
// return a void* and it's cast back to KeyValuePair* on the other side.
extern void *v8_BurstPersistent(ContextPtr ctx, PersistentValuePtr persistent,
                                int *out_numKeys);

// Returns a constant error string on errors, otherwise a NULL.  The error msg
// should NOT be freed by the caller.
extern const char *v8_setPersistentField(ContextPtr ctx,
                                         PersistentValuePtr persistent,
                                         const char *field,
                                         PersistentValuePtr value);

extern void v8_release_persistent(ContextPtr ctx,
                                  PersistentValuePtr persistent);

extern char *v8_error(ContextPtr ctx);

extern void v8_throw(ContextPtr ctx, char *errmsg);

extern void v8_terminate(IsolatePtr iso);

#ifdef __cplusplus
}
#endif

#endif  // !defined(V8WRAP_H)
