cmake_minimum_required(VERSION 3.16)
project(blynkLog_sub LANGUAGES C)

message(STATUS "Configuring BlynkLog build...")

add_executable(blynkLog blynkLog.c config.c) 
message(STATUS "  + Added executable: blynkLog from blynkLog.c and config.c")

find_package(PkgConfig REQUIRED)
pkg_check_modules(JSONC REQUIRED json-c)
message(STATUS "  + Found json-c: Includes=${JSONC_INCLUDE_DIRS}, Libs=${JSONC_LIBRARIES}")

target_include_directories(blynkLog PRIVATE ${JSONC_INCLUDE_DIRS})
target_include_directories(blynkLog PRIVATE ${CMAKE_SOURCE_DIR}/include)
message(STATUS "  + Added include directories: ${JSONC_INCLUDE_DIRS}, ${CMAKE_SOURCE_DIR}/include")

set(MYLIB_DIR "/home/pi/MWPCore/mylib" CACHE PATH "Path to pre-built mylib directory")
target_link_directories(blynkLog PRIVATE ${MYLIB_DIR})
message(STATUS "  + Added mylib link directory: ${MYLIB_DIR}")

target_link_directories(blynkLog PRIVATE ${PAHO_MQTT_C_LIB_DIR})
message(STATUS "  + Added Paho link directory: ${PAHO_MQTT_C_LIB_DIR}")

target_link_libraries(blynkLog PRIVATE paho-mqtt3c paho-mqtt3a paho-mqtt3as paho-mqtt3cs)
target_link_libraries(blynkLog PRIVATE mylib)
target_link_libraries(blynkLog PRIVATE ${JSONC_LIBRARIES})
message(STATUS "  + Linked libraries: paho-mqtt3*, mylib, ${JSONC_LIBRARIES}")

# Installation
# Install directly to the project's bin directory
install(
    TARGETS blynkLog
    RUNTIME DESTINATION /home/pi/MWPLog/bin # Hardcoded path
)
message(STATUS "  + Configured installation to: /home/pi/MWPLog/bin")

message(STATUS "BlynkLog configuration complete.")