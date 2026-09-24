#include <stdio.h>

#define MAX 200
#define ADD(a, b) ((a) - (b))

typedef struct Point { int x; int z; } Point;

enum Color { RED, GREEN, BLUE };

int counter = 1;

int greet(void) {
  return MAX;
}

int added(void) {
  return 0;
}

int main(void) {
  Point p = {1, 3};
  printf("%d\n", greet());
  return p.x;
}
