#include "config.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <json-c/json.h>

int load_config(const char* filename, Config* config) {
    json_object *root;
    json_object *blynk_obj, *data_filter_obj, *pin_config_obj;
    json_object *controllers_array, *zones_array;
    json_object *temp_obj;

    // Initialize the config structure
    memset(config, 0, sizeof(Config));

    // Read and parse the JSON file
    root = json_object_from_file(filename);
    if (!root) {
        fprintf(stderr, "Failed to parse config file: %s\n", filename);
        return -1;
    }

    // Parse Blynk settings
    if (!json_object_object_get_ex(root, "blynk", &blynk_obj)) {
        fprintf(stderr, "Missing 'blynk' section in config\n");
        json_object_put(root);
        return -1;
    }

    if (!json_object_object_get_ex(blynk_obj, "device_name", &temp_obj) ||
        !json_object_object_get_ex(blynk_obj, "template_id", &temp_obj) ||
        !json_object_object_get_ex(blynk_obj, "auth_token", &temp_obj) ||
        !json_object_object_get_ex(blynk_obj, "address", &temp_obj) ||
        !json_object_object_get_ex(blynk_obj, "client_id", &temp_obj) ||
        !json_object_object_get_ex(blynk_obj, "template_name", &temp_obj) ||
        !json_object_object_get_ex(blynk_obj, "topic", &temp_obj)) {
        fprintf(stderr, "Missing required Blynk settings\n");
        json_object_put(root);
        return -1;
    }

    json_object_object_get_ex(blynk_obj, "address", &temp_obj);
    config->blynk.address = strdup(json_object_get_string(temp_obj));

    json_object_object_get_ex(blynk_obj, "client_id", &temp_obj);
    config->blynk.client_id = strdup(json_object_get_string(temp_obj));

    json_object_object_get_ex(blynk_obj, "device_name", &temp_obj);
    config->blynk.device_name = strdup(json_object_get_string(temp_obj));
    
    json_object_object_get_ex(blynk_obj, "template_name", &temp_obj);
    config->blynk.template_name = strdup(json_object_get_string(temp_obj));

    json_object_object_get_ex(blynk_obj, "template_id", &temp_obj);
    config->blynk.template_id = strdup(json_object_get_string(temp_obj));

    json_object_object_get_ex(blynk_obj, "auth_token", &temp_obj);
    config->blynk.auth_token = strdup(json_object_get_string(temp_obj));

    json_object_object_get_ex(blynk_obj, "topic", &temp_obj);
    config->blynk.topic = strdup(json_object_get_string(temp_obj));

    // Parse data filter settings
    if (!json_object_object_get_ex(root, "data_filter", &data_filter_obj)) {
        fprintf(stderr, "Missing 'data_filter' section in config\n");
        json_object_put(root);
        return -1;
    }

    if (!json_object_object_get_ex(data_filter_obj, "controllers", &controllers_array) ||
        !json_object_object_get_ex(data_filter_obj, "zones", &zones_array)) {
        fprintf(stderr, "Missing required data filter arrays\n");
        json_object_put(root);
        return -1;
    }

    // Get controllers array
    config->data_filter.controllers_count = json_object_array_length(controllers_array);
    config->data_filter.controllers = malloc(config->data_filter.controllers_count * sizeof(int));
    for (int i = 0; i < config->data_filter.controllers_count; i++) {
        json_object *item = json_object_array_get_idx(controllers_array, i);
        config->data_filter.controllers[i] = json_object_get_int(item);
    }

    // Get zones array
    config->data_filter.zones_count = json_object_array_length(zones_array);
    config->data_filter.zones = malloc(config->data_filter.zones_count * sizeof(int));
    for (int i = 0; i < config->data_filter.zones_count; i++) {
        json_object *item = json_object_array_get_idx(zones_array, i);
        config->data_filter.zones[i] = json_object_get_int(item);
    }

    // Parse pin configuration
    if (!json_object_object_get_ex(root, "pin_config", &pin_config_obj)) {
        fprintf(stderr, "Missing 'pin_config' section in config\n");
        json_object_put(root);
        return -1;
    }

    if (!json_object_object_get_ex(pin_config_obj, "base_offset", &temp_obj)) {
        fprintf(stderr, "Missing 'base_offset' in pin_config\n");
        json_object_put(root);
        return -1;
    }
    config->pin_config.base_offset = json_object_get_int(temp_obj);

    json_object_put(root);
    return 0;
}

void free_config(Config* config) {
    if (config == NULL) {
        return;
    }

    // Free Blynk settings
    free(config->blynk.address);
    free(config->blynk.client_id);
    free(config->blynk.device_name);
    free(config->blynk.template_name);
    free(config->blynk.template_id);
    free(config->blynk.auth_token);
    free(config->blynk.topic);

    // Free data filter settings
    if (config->data_filter.controllers != NULL) {
        free(config->data_filter.controllers);
    }
    if (config->data_filter.zones != NULL) {
        free(config->data_filter.zones);
    }

    // Free pin configuration
    memset(config, 0, sizeof(Config));
} 