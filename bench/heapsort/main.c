// heapsort: in-place heapsort of N pseudo-random int64 values from a 64-bit
// LCG. Measures data-dependent branches, random memory access, and uint64
// wrap-around multiply. Prints a sortedness flag and two hashes of the result.
#include <stdint.h>
#include <stdio.h>

#define N 5000000

static int64_t data[N];

static uint64_t seed = 42;

static int64_t nextRand(void) {
    seed = seed * 6364136223846793005ULL + 1442695040888963407ULL;
    return (int64_t)(seed >> 33);
}

static void siftDown(int64_t start, int64_t end) {
    int64_t root = start;
    for (;;) {
        int64_t child = 2 * root + 1;
        if (child > end) {
            return;
        }
        int64_t swap = root;
        if (data[swap] < data[child]) {
            swap = child;
        }
        if (child + 1 <= end && data[swap] < data[child + 1]) {
            swap = child + 1;
        }
        if (swap == root) {
            return;
        }
        int64_t t = data[root];
        data[root] = data[swap];
        data[swap] = t;
        root = swap;
    }
}

static void heapsort(int64_t n) {
    for (int64_t start = (n - 2) / 2; start >= 0; start--) {
        siftDown(start, n - 1);
    }
    for (int64_t end = n - 1; end > 0; end--) {
        int64_t t = data[0];
        data[0] = data[end];
        data[end] = t;
        siftDown(0, end - 1);
    }
}

int main(void) {
    for (int64_t i = 0; i < N; i++) {
        data[i] = nextRand();
    }
    heapsort(N);
    int64_t sorted = 1;
    uint64_t hash = 0;
    for (int64_t i = 0; i < N; i++) {
        if (i > 0 && data[i - 1] > data[i]) {
            sorted = 0;
        }
        hash = hash * 31 + (uint64_t)data[i];
    }
    printf("%lld\n", (long long)sorted);
    printf("%lld\n", (long long)data[N / 2]);
    printf("%lld\n", (long long)(int64_t)(hash % 1000000007));
    return 0;
}
