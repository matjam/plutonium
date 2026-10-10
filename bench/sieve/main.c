// sieve: sieve of Eratosthenes over a large bool array, repeated.
// Measures byte stores with a stride, branchy scans, and memory bandwidth.
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>

#define N 20000000
#define REPS 50

static bool flags[N];

static int64_t sieve(void) {
    for (int64_t i = 0; i < N; i++) {
        flags[i] = true;
    }
    flags[0] = false;
    flags[1] = false;
    for (int64_t i = 2; i * i < N; i++) {
        if (flags[i]) {
            for (int64_t j = i * i; j < N; j += i) {
                flags[j] = false;
            }
        }
    }
    int64_t count = 0;
    for (int64_t i = 0; i < N; i++) {
        if (flags[i]) {
            count++;
        }
    }
    return count;
}

int main(void) {
    int64_t total = 0;
    int64_t last = 0;
    for (int64_t r = 0; r < REPS; r++) {
        last = sieve();
        total += last;
    }
    printf("%lld\n", (long long)last);
    printf("%lld\n", (long long)total);
    return 0;
}
