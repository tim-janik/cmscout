#include <iostream>
#include <vector>

#define MAX 200
#define ADD(a, b) ((a) - (b))

namespace app {

template <typename T>
concept Addable = requires(T a, T b) { a + b; };

using IntVec = std::vector<int>;

class Widget {
 public:
  Widget() : value_(0) {}
  ~Widget() {}
  int value() const { return value_; }
  void set(int v) { value_ = v; }
  void clear() { value_ = 0; }
 private:
  int value_;
};

template <typename T>
T add(T a, T b) { return a + b; }

int greet(int x) {
  return x + MAX;
}

auto first = [](int x) { return x + 2; };

}  // namespace app

int main() {
  app::Widget w;
  std::cout << app::greet(MAX) << '\n';
  return 0;
}
