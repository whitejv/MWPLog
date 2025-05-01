# /home/pi/MWPLog/CMakeLists.txt

cmake_minimum_required(VERSION 3.16)
project(MWPLog LANGUAGES C CXX) # Start with C/CXX, Go will be handled via custom commands initially

# --- Build Configuration ---
# Default to Release build if not specified externally (e.g., by cmake -DCMAKE_BUILD_TYPE=Debug ..)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release CACHE STRING "Build type (Release, Debug)" FORCE)
endif()
message(STATUS "Project: ${PROJECT_NAME}")
message(STATUS "Build type: ${CMAKE_BUILD_TYPE}")

# --- Common Variables ---
# Installation directory for executables (now hardcoded in install commands)
# set(INSTALL_BIN_DIR bin CACHE PATH "Installation directory for executables")
# message(STATUS "Install executables to: ${INSTALL_BIN_DIR}")

# Allow overriding Paho path via CMake command line (-DPAHO_MQTT_C_LIB_DIR=...)
# Default to the hardcoded path from BlynkLog's example for now
set(PAHO_MQTT_C_LIB_DIR "/home/pi/paho.mqtt.c/build/output/lib" CACHE PATH "Path to Paho MQTT C pre-built libraries")
message(STATUS "Using Paho MQTT C Libs from: ${PAHO_MQTT_C_LIB_DIR}") # Note: This should ideally be found properly later

# Set standard for C if needed (e.g., C99)
# set(CMAKE_C_STANDARD 99)
# set(CMAKE_C_STANDARD_REQUIRED True)

# --- Build Custom C Library ---
# mylib is pre-built externally, only link against it where needed.
# Removing add_subdirectory for it.
# message(STATUS "Adding subdirectory: mylib from /home/pi/MWPCore/myLib")
# add_subdirectory(/home/pi/MWPCore/myLib ${CMAKE_BINARY_DIR}/mylib_build)

# --- Build Go Components ---
# These will have their own CMakeLists.txt defining custom build commands
message(STATUS "Adding subdirectory: mwp_data_service")
add_subdirectory(mwp_data_service)

message(STATUS "Adding subdirectory: log_data")
add_subdirectory(log_data)

# --- Build C Components ---
# Requires BlynkLog/CMakeLists.txt to exist
message(STATUS "Adding subdirectory: BlynkLog")
add_subdirectory(BlynkLog)

# Requires SysLog/CMakeLists.txt to exist - Temporarily disabled as it's a reference
# message(STATUS "Adding subdirectory: SysLog")
# add_subdirectory(SysLog)

message(STATUS "Root CMakeLists.txt processing complete.") 