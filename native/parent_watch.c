// MIT, Copyright (c) 2026 sonjiwu2. Upstream ByeDPI sources stay unmodified.
// A pipe owned only by the parent process closes even on SIGKILL. Unlike
// PR_SET_PDEATHSIG this does not depend on the lifetime of a Go/Java thread.
#include <errno.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static void *watch_parent(void *unused) {
    (void)unused;
    char byte;
    for (;;) {
        ssize_t n = read(STDIN_FILENO, &byte, 1);
        if (n > 0 || (n < 0 && errno == EINTR)) continue;
        _exit(0);
    }
    return NULL;
}

__attribute__((constructor)) static void start_parent_watch(void) {
    const char *enabled = getenv("YOUTUBE_PLUGIN_PARENT_WATCH");
    if (!enabled || strcmp(enabled, "1") != 0) return;
    pthread_t thread;
    if (pthread_create(&thread, NULL, watch_parent, NULL) != 0) _exit(1);
    pthread_detach(thread);
}
