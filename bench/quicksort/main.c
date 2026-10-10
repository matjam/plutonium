// quicksort: recursive Hoare-partition quicksort (middle pivot) of N
// pseudo-random int64 values from a 64-bit LCG. Measures recursion,
// unpredictable branches, and sequential memory scans.
// Prints a sortedness flag, the median, and a hash of the sorted array.
#include <stdint.h>
#include <stdio.h>

#define N 10000000

static int64_t data[N];

static uint64_t seed = 42;

static int64_t nextRand(void) {
    seed = seed * 6364136223846793005ULL + 1442695040888963407ULL;
    return (int64_t)(seed >> 33);
}

static void quicksort(int64_t lo, int64_t hi) {
    if (lo >= hi) {
        return;
    }
    int64_t pivot = data[lo + (hi - lo) / 2];
    int64_t i = lo;
    int64_t j = hi;
    while (i <= j) {
        while (data[i] < pivot) {
            i++;
        }
        while (data[j] > pivot) {
            j--;
        }
        if (i <= j) {
            int64_t t = data[i];
            data[i] = data[j];
            data[j] = t;
            i++;
            j--;
        }
    }
    if (lo < j) {
        quicksort(lo, j);
    }
    if (i < hi) {
        quicksort(i, hi);
    }
}

int main(void) {
    for (int64_t i = 0; i < N; i++) {
        data[i] = nextRand();
    }
    quicksort(0, N - 1);
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
