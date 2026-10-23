cmake_minimum_required(VERSION 3.22)
project(razgon_ciadpi C)
set(CMAKE_C_STANDARD 99)
add_executable(ciadpi byedpi/main.c byedpi/packets.c byedpi/conev.c
    byedpi/proxy.c byedpi/desync.c byedpi/mpool.c byedpi/extend.c)
target_include_directories(ciadpi PRIVATE byedpi)
target_compile_definitions(ciadpi PRIVATE _DEFAULT_SOURCE)
target_compile_options(ciadpi PRIVATE -O2 -fPIE -Wall -Wno-unused-parameter)
target_link_options(ciadpi PRIVATE -pie -Wl,-z,max-page-size=16384)
# Android разрешает запуск исполняемого файла из распакованного nativeLibraryDir.
set_target_properties(ciadpi PROPERTIES OUTPUT_NAME libciadpi.so)
