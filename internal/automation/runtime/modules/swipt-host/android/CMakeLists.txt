cmake_minimum_required(VERSION 3.13)
project(swipt-runtime)

set(CMAKE_CXX_STANDARD 20)

find_package(ReactAndroid REQUIRED CONFIG)

add_library(swipt-runtime SHARED src/main/cpp/JavaScriptInterrupter.cpp)
target_link_libraries(swipt-runtime ReactAndroid::jsi)
