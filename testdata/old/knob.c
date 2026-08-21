#include <stdio.h>

#define MAX 100
#define ADD(a, b) ((a) + (b))

typedef struct Point { int x; int y; } Point;

enum Color { RED, GREEN, BLUE };

int counter = 0;

int greet(void) {
  return MAX;
}

int add_real(int a, int b) {
  return ADD(a, b);
}

int main(void) {
  Point p = {1, 2};
  printf("%d\n", greet());
  return p.x;
}
