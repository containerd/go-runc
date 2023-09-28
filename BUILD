go_library(
    name = "runc",
    srcs = [
        "command_linux.go",
        "command_other.go",
        "console.go",
        "container.go",
        "events.go",
        "io.go",
        "io_unix.go",
        "io_windows.go",
        "monitor.go",
        "runc.go",
        "utils.go",
    ],
)

go_test(
    name = "runc_test",
    srcs = [
        "console_test.go",
        "runc_test.go",
    ],
    library = ":runc",
)
