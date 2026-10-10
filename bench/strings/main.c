// strings: wc-style scan of a generated text buffer passed as pointer+length.
// Counts lines, words and bytes, tracks the longest word, counts words equal
// to a string constant, and folds an FNV-1a hash of every word into a running
// hash. One byte of the buffer changes before each pass.
// Measures byte indexing, compare chains, string indexing and struct return
// by value.
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define N 16000000
#define REPS 35

static const char *const pattern = "go";

static uint8_t text[N];

static uint64_t seed = 42;

static int64_t nextRand(void) {
    seed = seed * 6364136223846793005ULL + 1442695040888963407ULL;
    return (int64_t)(seed >> 33);
}

typedef struct {
    int64_t Lines;
    int64_t Words;
    int64_t Bytes;
    int64_t Longest;
    int64_t Matches;
    uint32_t Hash;
} Stats;

static void generate(void) {
    for (int64_t i = 0; i < N; i++) {
        int64_t r = nextRand() % 64;
        if (r < 10) {
            text[i] = ' ';
        } else if (r == 10) {
            text[i] = '\n';
        } else {
            text[i] = (uint8_t)('a' + r % 26);
        }
    }
}

static bool isSpace(uint8_t c) {
    return c == ' ' || c == '\n' || c == '\t' || c == '\r';
}

static bool matches(const uint8_t *b, size_t start, size_t end, const char *pat, size_t patLen) {
    if (end - start != patLen) {
        return false;
    }
    for (size_t i = 0; i < patLen; i++) {
        if (b[start + i] != (uint8_t)pat[i]) {
            return false;
        }
    }
    return true;
}

static Stats scan(const uint8_t *b, size_t n) {
    Stats s = {0};
    size_t patLen = strlen(pattern);
    bool inWord = false;
    size_t start = 0;
    uint32_t wordHash = 0;
    for (size_t i = 0; i < n; i++) {
        uint8_t c = b[i];
        if (c == '\n') {
            s.Lines++;
        }
        if (isSpace(c)) {
            if (inWord) {
                s.Words++;
                s.Hash = s.Hash * 31 + wordHash;
                if ((int64_t)(i - start) > s.Longest) {
                    s.Longest = (int64_t)(i - start);
                }
                if (matches(b, start, i, pattern, patLen)) {
                    s.Matches++;
                }
                inWord = false;
            }
        } else {
            if (!inWord) {
                inWord = true;
                start = i;
                wordHash = 2166136261u;
            }
            wordHash = (wordHash ^ (uint32_t)c) * 16777619u;
        }
    }
    if (inWord) {
        s.Words++;
        s.Hash = s.Hash * 31 + wordHash;
        if ((int64_t)(n - start) > s.Longest) {
            s.Longest = (int64_t)(n - start);
        }
        if (matches(b, start, n, pattern, patLen)) {
            s.Matches++;
        }
    }
    s.Bytes = (int64_t)n;
    return s;
}

int main(void) {
    generate();
    Stats s = {0};
    int64_t totalWords = 0;
    uint32_t hashXor = 0;
    for (int64_t r = 0; r < REPS; r++) {
        text[(r * 7919) % N] = (uint8_t)('a' + r % 26);
        s = scan(text, N);
        totalWords += s.Words;
        hashXor ^= s.Hash;
    }
    printf("%lld\n", (long long)s.Lines);
    printf("%lld\n", (long long)s.Words);
    printf("%lld\n", (long long)s.Bytes);
    printf("%lld\n", (long long)s.Longest);
    printf("%lld\n", (long long)s.Matches);
    printf("%lld\n", (long long)s.Hash);
    printf("%lld\n", (long long)totalWords);
    printf("%lld\n", (long long)hashXor);
    return 0;
}
