#ifndef CONFIG_H
#define CONFIG_H

#include <json-c/json.h>

// Configuration structure
typedef struct {
    struct {
        char* address;
        char* client_id;
        char* device_name;
        char* template_name;
        char* template_id;
        char* auth_token;
        char* topic;
    } blynk;
    
    struct {
        int* controllers;
        int controllers_count;
        int* zones;
        int zones_count;
    } data_filter;
    
    struct {
        int base_offset;
    } pin_config;
} Config;

// Function declarations
int load_config(const char* config_file, Config* config);
void free_config(Config* config);

#endif // CONFIG_H 