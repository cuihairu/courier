#!/bin/sh
# UE 端零依赖测试入口:ISO C++17 独立编译(无引擎依赖),g++ 直接跑。
# 用法:sh sdks/ue/run_tests.sh
# 注:UE 引擎集成面(HttpModule 适配、UObject 包装、.uplugin)不在本脚本范围,
#     需真机/引擎环境;此处验证平台无关的 contract/core/service 逻辑层。
set -e
cd "$(dirname "$0")"

CXX="${CXX:-g++}"
FLAGS="-std=c++17 -Wall -Wextra"
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

run() {
    name="$1"
    shift
    printf '== %s\n' "$name"
    "$CXX" $FLAGS "$@" -o "$OUT/$name"
    "$OUT/$name"
}

run contract contract/contract_test.cpp -Icontract
run json core/json_test.cpp -Icore -Icontract
run core core/core_test.cpp -Icore -Icontract
run lifecycle core/lifecycle_test.cpp -Icore -Icontract
run service service/service_test.cpp -Iservice -Icore -Icontract
run service2 service/service2_test.cpp -Iservice -Icore -Icontract
