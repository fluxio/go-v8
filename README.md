Go-V8 Bindings
===

The go-V8 bindings allow a user to execute javascript from within a go
executable.

The bindings are tested to work with v8 build 4.8.271.17 (latest stable at the
time of writing).

Please see `v8_test.go` for examples of usage.

Compiling
---

In order for the bingings to compile correctly, one needs to:

1. Compile v8 as a static library or just use a pre-built version.
2. Let cgo know where the library is located.

Lets say you've checked out go-v8 into `$GO_V8` and want to place
the static v8 library into `$GO_V8/libv8/`.

### Recommended: use a pre-built static library

Just grab the pre-built artifacts:

    cd $GO_V8
    git clone https://github.com/forwidur/libv8

### Compiling v8 from scratch

Check out [v8](https://github.com/v8/v8/wiki/Using%20Git) into a directory,
referred to here as `$V8`.

#### Linux

Build:

    cd $V8
    make x64.release GYPFLAGS="-Dv8_use_external_startup_data=0 \
      -Dv8_enable_i18n_support=0 -Dv8_enable_gdbjit=0"`

If build system produces a thin archive, you want to make it into a fat one:

    for lib in `find out/x64.release/obj.target/tools/gyp/ -name '*.a'`;
      do ar -t $lib | xargs ar rvs $lib.new && mv -v $lib.new $lib;
    done`


Copy the libraries to the destination directory:

    cp -v out/x64.release/obj.target/tools/gyp/libv8_{base,libbase,external,libplatform}* \
      ${GO_V8}/libv8/

#### Mac

To build:

    cd $V8
    CXX="`which clang++` -std=c++11 -stdlib=libc++" \
    GYP_DEFINES="mac_deployment_target=10.10" \
    make x64.release GYPFLAGS="-Dv8_use_external_startup_data=0 \
      -Dv8_enable_i18n_support=0 -Dv8_enable_gdbjit=0"

Copy the libraries to the destination directory:

    cp -v out/x64.release/libv8_{base,libbase,external,libplatform}* \
      ${GO_V8}/libv8/x86_64-apple-darwin

Note: On MacOS, the resulting libraries contain debugging information by default
(even though we've built the release version). As a result, the binaries are 30x
larger, then they should be. Strip that with `strip -S out/x64.release/libv8*.a`
to reduce the size of the archives very significantly.

Good luck!

#### V8 compile dependencies

The list of v8 includes (found in `$V8/include`) the bindings depend on:

    libplatform/libplatform.h
    v8-testing.h
    v8.h
    v8config.h
    v8-version.h
    v8-platform.h
    v8-debug.h
    v8-profiler.h

Copy those to `$GO_V8/libv8/include/` (or just set the `-I` to `$V8/include`).

### Let cgo know where the library is located

Let cgo know where it should look for the libraries do:

    export CGO_CXXFLAGS="-I $GO_V8/libv8/include"
    export CGO_LDFLAGS="-L $GO_V8/libv8"

License
-------

This software is licensed under Apache License, Version 2.0. Please see
[LICENSE](https://raw.githubusercontent.com/fluxio/go-v8/master/LICENSE)
for more information.

Copyright© 2016, Flux Factory Inc.
