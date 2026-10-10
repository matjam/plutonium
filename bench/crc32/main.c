// crc32: byte-at-a-time table-driven CRC-32 (IEEE, reflected polynomial
// 0xEDB88320) over an LCG-filled buffer, repeated. One byte of the buffer is
// changed before each pass so the passes cannot be merged.
// Measures uint32 shifts/xors, table lookups and slice passing.
// Prints the CRC of the final pass and an xor of all pass CRCs.
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>

#define N 16000000
#define REPS 36

static uint32_t table[256];
static uint8_t buf[N];

static uint64_t seed = 42;

static int64_t nextRand(void) {
    seed = seed * 6364136223846793005ULL + 1442695040888963407ULL;
    return (int64_t)(seed >> 33);
}

static void makeTable(void) {
    for (int i = 0; i < 256; i++) {
        uint32_t c = (uint32_t)i;
        for (int k = 0; k < 8; k++) {
            if ((c & 1) != 0) {
                c = 0xEDB88320u ^ (c >> 1);
            } else {
                c = c >> 1;
            }
        }
        table[i] = c;
    }
}

static uint32_t crc32(const uint8_t *b, size_t n) {
    uint32_t c = 0xFFFFFFFFu;
    for (size_t i = 0; i < n; i++) {
        c = table[(c ^ (uint32_t)b[i]) & 0xFF] ^ (c >> 8);
    }
    return c ^ 0xFFFFFFFFu;
}

int main(void) {
    makeTable();
    for (int64_t i = 0; i < N; i++) {
        buf[i] = (uint8_t)(nextRand() >> 23);
    }
    uint32_t acc = 0;
    uint32_t last = 0;
    for (int64_t r = 0; r < REPS; r++) {
        buf[(r * 7919) % N] = (uint8_t)r;
        last = crc32(buf, N);
        acc ^= last;
    }
    printf("%lld\n", (long long)last);
    printf("%lld\n", (long long)acc);
    return 0;
}
