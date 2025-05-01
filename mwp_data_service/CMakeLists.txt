# /home/pi/MWPLog/mwp_data_service/CMakeLists.txt

# Find the Go compiler
find_program(GO_EXECUTABLE go REQUIRED)
message(STATUS "Found Go compiler: ${GO_EXECUTABLE}")

# Define the output executable name and path
set(EXECUTABLE_NAME "mwp_data_service")
set(OUTPUT_EXECUTABLE "${CMAKE_CURRENT_BINARY_DIR}/${EXECUTABLE_NAME}")

# Define the main Go package source directory relative to this CMakeLists.txt
set(SOURCE_DIR "${CMAKE_CURRENT_SOURCE_DIR}/cmd/mwp_data_service")

# --- Custom build command for Go ---
# Note: Ideally, we'd get all .go files, but Go handles dependencies.
# Building the main package is usually sufficient.
add_custom_command(
    OUTPUT ${OUTPUT_EXECUTABLE}
    # Command runs within the module's root directory
    COMMAND ${GO_EXECUTABLE} build -o ${OUTPUT_EXECUTABLE} ${SOURCE_DIR}
    WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR} # Run 'go build' from the module root
    DEPENDS ${SOURCE_DIR}/main.go # Basic dependency, Go handles the rest
    COMMENT "Building Go application ${EXECUTABLE_NAME}"
    VERBATIM # Ensure commands are executed as written
)

# --- Custom target for the Go build ---
add_custom_target(
    ${EXECUTABLE_NAME}_build ALL # Build this target by default
    DEPENDS ${OUTPUT_EXECUTABLE}
    COMMENT "Ensuring ${EXECUTABLE_NAME} is built"
)

# --- Installation ---
# Install directly to the project's bin directory
install(
    PROGRAMS ${OUTPUT_EXECUTABLE}
    DESTINATION /home/pi/MWPLog/bin # Hardcoded path
    RENAME ${EXECUTABLE_NAME}
)

message(STATUS "Configured Go build for ${EXECUTABLE_NAME}") 