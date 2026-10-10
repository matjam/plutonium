// binarytrees: Benchmarks Game binary-trees. Allocates and walks many short-
// lived perfect binary trees plus one long-lived tree.
// Measures allocation/deallocation throughput and pointer chasing.
//
// Memory management differs by language and is part of what is measured:
// Go relies on its garbage collector, C calls malloc/free, Rust drops Box
// values, Plutonium frees through its compiler-inserted ownership scheme.
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#define MAX_DEPTH 18

typedef struct Node {
    struct Node *left;
    struct Node *right;
} Node;

static Node *bottomUpTree(int64_t depth) {
    Node *n = malloc(sizeof(Node));
    if (n == NULL) {
        abort();
    }
    n->left = NULL;
    n->right = NULL;
    if (depth > 0) {
        n->left = bottomUpTree(depth - 1);
        n->right = bottomUpTree(depth - 1);
    }
    return n;
}

static int64_t itemCheck(const Node *n) {
    if (n->left == NULL) {
        return 1;
    }
    return 1 + itemCheck(n->left) + itemCheck(n->right);
}

static void freeTree(Node *n) {
    if (n->left != NULL) {
        freeTree(n->left);
        freeTree(n->right);
    }
    free(n);
}

static int64_t stretch(int64_t depth) {
    Node *t = bottomUpTree(depth);
    int64_t c = itemCheck(t);
    freeTree(t);
    return c;
}

int main(void) {
    int64_t minDepth = 4;
    int64_t maxDepth = MAX_DEPTH;
    if (minDepth + 2 > maxDepth) {
        maxDepth = minDepth + 2;
    }

    printf("%lld %lld\n", (long long)(maxDepth + 1), (long long)stretch(maxDepth + 1));

    Node *longLived = bottomUpTree(maxDepth);

    for (int64_t d = minDepth; d <= maxDepth; d += 2) {
        int64_t iterations = (int64_t)1 << (maxDepth - d + minDepth);
        int64_t check = 0;
        for (int64_t i = 0; i < iterations; i++) {
            Node *t = bottomUpTree(d);
            check += itemCheck(t);
            freeTree(t);
        }
        printf("%lld %lld %lld\n", (long long)iterations, (long long)d, (long long)check);
    }

    printf("%lld %lld\n", (long long)maxDepth, (long long)itemCheck(longLived));
    freeTree(longLived);
    return 0;
}
