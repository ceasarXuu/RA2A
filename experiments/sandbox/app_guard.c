/* Protect only the new private Electron processes, not native writer metadata. */
#include <string.h>
#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <stdio.h>
#include <sys/prctl.h>
#include <unistd.h>

static void *activate(void *unused) {
    (void)unused;
    for (;;) {
        int trigger = open("/run/app-control/activate", O_RDONLY | O_CLOEXEC);
        if (trigger >= 0) {
            close(trigger);
            if (prctl(PR_SET_DUMPABLE, 0, 0, 0, 0) != 0)
                _exit(125);
            int ack = open("/run/app-control/protected", O_CREAT | O_EXCL | O_WRONLY, 0600);
            char identity[32];
            int length = snprintf(identity, sizeof(identity), "%ld", (long)getpid());
            if (ack < 0 || write(ack, identity, length) != length)
                _exit(125);
            close(ack);
            return NULL;
        }
        if (errno != ENOENT)
            _exit(125);
        usleep(10000);
    }
}

__attribute__((constructor)) static void protect_app(void) {
    char executable[4096];
    ssize_t length = readlink("/proc/self/exe", executable, sizeof(executable) - 1);
    if (length < 0 || length >= (ssize_t)sizeof(executable) - 1)
        _exit(125);
    executable[length] = '\0';
    if (strcmp(executable, "/usr/lib/chatgpt/ChatGPT") != 0)
        return;
    char arguments[8192];
    int fd = open("/proc/self/cmdline", O_RDONLY | O_CLOEXEC);
    ssize_t size = fd < 0 ? -1 : read(fd, arguments, sizeof(arguments));
    if (fd >= 0) close(fd);
    if (size < 0 || size == sizeof(arguments))
        _exit(125);
    for (ssize_t i = 0; i + 7 <= size; i++)
        if ((i == 0 || arguments[i-1] == '\0') && memcmp(arguments+i, "--type=", 7) == 0)
            return;
    pthread_t thread;
    if (pthread_create(&thread, NULL, activate, NULL) != 0 || pthread_detach(thread) != 0)
        _exit(125);
}
