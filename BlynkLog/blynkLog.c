#define ADDRESS     "blynk.cloud:1883"   // Blynk MQTT Server
#define CLIENTID    "Log Blynk"               // Unique client ID for your device

#define BLYNK_TEMPLATE_ID "TMPL2XybQTUE8"
#define BLYNK_TEMPLATE_NAME "MWP Log" // Updated Template Name
#define BLYNK_AUTH_TOKEN "RWwXuvg7SaK_-GuNRdypahmTgVHgLQoj" // Updated Auth Token

#define BLYNK_TOPIC       "batch_ds" // Topic for Virtual Pin V1
#define BLYNK_DEVICE_NAME "device"

#define BLYNK_PROTOTYPE_ROW_LIMIT 16 // Set to BLYNK_TABLE_ROW_COUNT to send all rows
#define BLYNK_BATCH_ROW_LIMIT 8      // Max rows per Blynk batch message

// New definitions for Blynk V0 and mwp_data_service
// #define BLYNK_DOWNLINK_V0_TOPIC_FORMAT "blynk/%s/downlink/virtual/pin/v0" // Old format
#define BLYNK_TIMEWINDOW_DOWNLINK_DS_TOPIC "downlink/ds/TimeWindow" // New format with datastream name
#define MWP_DATA_SERVICE_QUERY_TOPIC "mwp/json/data/log/dataservice/query_request"

// Placeholder for the topic where mwp_data_service publishes the watertable JSON
#define MWP_WATERTABLE_JSON_TOPIC "mwp/json/data/log/dataservice/query_results" // <<< USER: Please confirm/update this topic

/* Define IP Address for MQTT for both
 * a Production Server and a Development Server
 */
#define PROD_MQTT_IP "192.168.1.250"
#define PROD_MQTT_PORT 1883
#define DEV_MQTT_IP "192.168.1.249"
#define DEV_MQTT_PORT 1883
#define QOS 0
#define TIMEOUT 10000L
#define TRUE 1
#define FALSE 0
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <math.h>
#include <time.h>
#include <json-c/json.h>
#include "unistd.h"
#include "MQTTClient.h"

// Moved to file scope
static MQTTClient client = NULL;
static volatile int blynkClient_initialized_and_connected = 0; // volatile as it can be changed by callback
static MQTTClient blynkClient = NULL; // Moved to file scope for access in msgarrvd

int verbose = FALSE;
int disc_finished = 0;
int subscribed = 0;
int finished = 0;

// Global flag for connection loss detection, to be set by connlost callback
volatile int connection_lost_flag = 0;
// Reconnect delay in seconds
#define RECONNECT_DELAY_SECONDS 5

// Time Window Mapping
typedef struct {
    int blynkValue;             // Integer value from Blynk menu
    const char* timeWindowString; // Corresponding string (e.g., "1h", "May")
} TimeWindowMapEntry;

static TimeWindowMapEntry timeWindowMap[] = {
    {0, "1h"}, {1, "12h"}, {2, "24h"}, {3, "48h"}, {4, "72h"},
    {5, "7d"}, {6, "14d"}, {7, "May"}, {8, "June"}, {9, "July"},
    {10, "August"}, {11, "September"}, {12, "2025"}, {13, "2026"},
    {14, "2027"}
    // Add more entries if V0 range is 0-20 and more items are added
};
static const int numTimeWindowEntries = sizeof(timeWindowMap) / sizeof(timeWindowMap[0]);

// Structures and table for storing data to be sent to Blynk (from mwp_data_service)

typedef struct {
    int   zone_number;    // The zone number to be displayed/sent for this row
    float total_flow;     // Gallons
    float total_minutes;  // Minutes
    float avg_psi;
    float gpm;
    int   data_valid;     // Flag to indicate if data was successfully populated from JSON for this row
} BlynkDataRow;

#define BLYNK_TABLE_ROW_COUNT 31
static BlynkDataRow blynk_display_table[BLYNK_TABLE_ROW_COUNT];

// Maps a specific Controller/Zone from the JSON to a row in blynk_display_table
typedef struct {
    const char* controller_json_key; // Key for the controller in JSON (e.g., "0", "1")
    const char* zone_json_key;       // Key for the zone within controller in JSON (e.g., "0", "1", "16")
    int         display_zone_number; // The actual zone number to store and eventually display
} ControllerZoneSource;

// Predefined map of 31 data sources from JSON to rows in our blynk_display_table
static const ControllerZoneSource json_to_blynk_map[BLYNK_TABLE_ROW_COUNT] = {
    // Controller 0 (1 row)
    { "0", "0", 0 },  // C0, Z0 -> displays Zone 0
    // Controller 1 (16 rows)
    { "1", "1", 1 },  // C1, Z1 -> displays Zone 1
    { "1", "2", 2 },  // C1, Z2 -> displays Zone 2
    { "1", "3", 3 },
    { "1", "4", 4 },
    { "1", "5", 5 },
    { "1", "6", 6 },
    { "1", "7", 7 },
    { "1", "8", 8 },
    { "1", "9", 9 },
    { "1", "10", 10 },
    { "1", "11", 11 },
    { "1", "12", 12 },
    { "1", "13", 13 },
    { "1", "14", 14 },
    { "1", "15", 15 },
    { "1", "16", 16 },
    // Controller 2 (13 rows)
    { "2", "1", 1 },  // C2, Z1 -> displays Zone 1
    { "2", "2", 2 },
    { "2", "3", 3 },
    { "2", "4", 4 },
    { "2", "5", 5 },
    { "2", "6", 6 },
    { "2", "7", 7 },
    { "2", "8", 8 },
    { "2", "9", 9 },
    { "2", "10", 10 },
    { "2", "11", 11 },
    { "2", "12", 12 },
    { "2", "13", 13 },
    // Controller 3 (1 row)
    { "3", "1", 1 }   // C3, Z1 -> displays Zone 1
};

MQTTClient_deliveryToken deliveredtoken;

void delivered(void *context, MQTTClient_deliveryToken dt)
{
   //printf("Message with token value %d delivery confirmed\n", dt);
   deliveredtoken = dt;
}

/* Using an include here to allow me to reuse a chunk of code that
   would not work as a library file. So treating it like an include to 
   copy and paste the same code into multiple programs. 
*/

int msgarrvd(void *context, char *topicName, int topicLen, MQTTClient_message *message)
{
    // Check if the message is for the blynkLog functionality (time window selection from Blynk)
    // This function is the callback for the *main client* that connects to the local broker.
    // The blynk_msgarrvd is for the blynkClient.
    // For now, this main client's msgarrvd will handle the incoming JSON for the table.

    printf("Main client message arrived on topic: %s\n", topicName);

    // Check if this is the message from mwp_data_service with the watertable JSON
    if (strcmp(topicName, MWP_WATERTABLE_JSON_TOPIC) == 0)
    {
        printf("Received watertable JSON data. Processing...\n");
        json_object *parsed_json = json_tokener_parse((const char *)message->payload);

        if (parsed_json == NULL)
        {
            fprintf(stderr, "Failed to parse watertable JSON string\n");
            MQTTClient_freeMessage(&message);
            MQTTClient_free(topicName);
            return 1;
        }

        json_object *details_obj;
        if (!json_object_object_get_ex(parsed_json, "details", &details_obj)) {
            fprintf(stderr, "JSON parsing error: 'details' key missing\n");
            json_object_put(parsed_json); // Free the root json object
            MQTTClient_freeMessage(&message);
            MQTTClient_free(topicName);
            return 1;
        }
        if (json_object_get_type(details_obj) != json_type_object) {
            fprintf(stderr, "JSON parsing error: 'details' is not an object\n");
            json_object_put(parsed_json);
            MQTTClient_freeMessage(&message);
            MQTTClient_free(topicName);
            return 1;
        }

        // Iterate through the 31 mapped rows and populate blynk_display_table
        for (int i = 0; i < BLYNK_TABLE_ROW_COUNT; i++) {
            const ControllerZoneSource* source = &json_to_blynk_map[i];
            BlynkDataRow* target_row = &blynk_display_table[i];
            
            // Initialize data_valid to false for this row
            target_row->data_valid = 0;
            target_row->zone_number = source->display_zone_number; // Pre-set the zone number for display

            json_object *controller_obj;
            if (!json_object_object_get_ex(details_obj, source->controller_json_key, &controller_obj) || 
                json_object_get_type(controller_obj) != json_type_object) {
                fprintf(stderr, "Could not find controller object for key: %s, or it's not an object. Skipping row %d.\n", 
                        source->controller_json_key, i);
                // Optionally set default/error values in target_row
                target_row->total_flow = 0.0f; target_row->total_minutes = 0.0f; target_row->avg_psi = 0.0f; target_row->gpm = 0.0f;
                continue; 
            }

            json_object *zone_obj;
            if (!json_object_object_get_ex(controller_obj, source->zone_json_key, &zone_obj) || 
                json_object_get_type(zone_obj) != json_type_object) {
                fprintf(stderr, "Could not find zone object for controller %s, zone key: %s. Skipping row %d.\n", 
                        source->controller_json_key, source->zone_json_key, i);
                target_row->total_flow = 0.0f; target_row->total_minutes = 0.0f; target_row->avg_psi = 0.0f; target_row->gpm = 0.0f;
                continue;
            }

            // Extract data fields from the zone_obj
            json_object *temp_obj;
            double total_flow = 0.0, total_seconds = 0.0, avg_psi = 0.0, gpm = 0.0;
            int updated = 0; // To check updatedInLastQuery

            if (json_object_object_get_ex(zone_obj, "updatedInLastQuery", &temp_obj) && json_object_get_type(temp_obj) == json_type_boolean) {
                updated = json_object_get_boolean(temp_obj);
            }
            
            // We could choose to only populate if `updated` is true, or if data is non-zero.
            // For now, we extract whatever is there. User can decide on filtering later.
            // if (!updated) { 
            //     fprintf(stdout, "Data for C:%s Z:%s not updated in last query. Row %d may contain stale or zero data.\n", 
            //             source->controller_json_key, source->zone_json_key, i);
            // }

            if (json_object_object_get_ex(zone_obj, "totalFlow", &temp_obj) && json_object_get_type(temp_obj) == json_type_double) {
                total_flow = json_object_get_double(temp_obj);
            }
            if (json_object_object_get_ex(zone_obj, "totalSeconds", &temp_obj) && json_object_get_type(temp_obj) == json_type_double) {
                total_seconds = json_object_get_double(temp_obj);
            }
            if (json_object_object_get_ex(zone_obj, "avgPSI", &temp_obj) && json_object_get_type(temp_obj) == json_type_double) {
                avg_psi = json_object_get_double(temp_obj);
            }
            if (json_object_object_get_ex(zone_obj, "gpm", &temp_obj) && json_object_get_type(temp_obj) == json_type_double) {
                gpm = json_object_get_double(temp_obj);
            }

            target_row->total_flow = (float)total_flow;
            target_row->total_minutes = (total_seconds > 0) ? (float)(total_seconds / 60.0) : 0.0f;
            target_row->avg_psi = (float)avg_psi;
            target_row->gpm = (float)gpm;
            target_row->data_valid = 1; // Mark data as successfully populated for this row

            if (verbose) {
                printf("Populated Blynk Table Row %d (C:%s, Z:%s, DispZ:%d): Flow=%.2f, Mins=%.2f, PSI=%.2f, GPM=%.2f\n",
                       i, source->controller_json_key, source->zone_json_key, target_row->zone_number,
                       target_row->total_flow, target_row->total_minutes, target_row->avg_psi, target_row->gpm);
            }
        }

        json_object_put(parsed_json); // Free the root json object
        printf("Finished processing watertable JSON data.\n");

        // Now, send data from blynk_display_table to Blynk using batch_ds via a loop
        if (blynkClient_initialized_and_connected && blynkClient != NULL) {
            int total_rows_to_process = (BLYNK_PROTOTYPE_ROW_LIMIT < BLYNK_TABLE_ROW_COUNT) ? BLYNK_PROTOTYPE_ROW_LIMIT : BLYNK_TABLE_ROW_COUNT;
            int row_index = 0;
            
            while (row_index < total_rows_to_process) {
                json_object *blynk_payload_obj = json_object_new_object();
                if (blynk_payload_obj == NULL) {
                    fprintf(stderr, "Failed to create JSON object for Blynk batch_ds payload.\n");
                    break; // Exit the sending loop
                }

                int items_added_to_this_payload = 0;
                char ds_name_buffer[16];

                // Inner loop: build one batch payload with up to BLYNK_BATCH_ROW_LIMIT valid rows
                for (int batch_item_count = 0; batch_item_count < BLYNK_BATCH_ROW_LIMIT && row_index < total_rows_to_process; row_index++) {
                    if (blynk_display_table[row_index].data_valid) {
                        // Zone
                        snprintf(ds_name_buffer, sizeof(ds_name_buffer), "V%dzone", row_index * 5 + 1);
                        json_object_object_add(blynk_payload_obj, ds_name_buffer, json_object_new_int(blynk_display_table[row_index].zone_number));
                        // Flow
                        snprintf(ds_name_buffer, sizeof(ds_name_buffer), "V%dflow", row_index * 5 + 2);
                        json_object_object_add(blynk_payload_obj, ds_name_buffer, json_object_new_double(blynk_display_table[row_index].total_flow));
                        // Minutes
                        snprintf(ds_name_buffer, sizeof(ds_name_buffer), "V%dmin", row_index * 5 + 3);
                        json_object_object_add(blynk_payload_obj, ds_name_buffer, json_object_new_double(blynk_display_table[row_index].total_minutes));
                        // PSI
                        snprintf(ds_name_buffer, sizeof(ds_name_buffer), "V%dpsi", row_index * 5 + 4);
                        json_object_object_add(blynk_payload_obj, ds_name_buffer, json_object_new_double(blynk_display_table[row_index].avg_psi));
                        // GPM
                        snprintf(ds_name_buffer, sizeof(ds_name_buffer), "V%dgpm", row_index * 5 + 5);
                        json_object_object_add(blynk_payload_obj, ds_name_buffer, json_object_new_double(blynk_display_table[row_index].gpm));
                        
                        items_added_to_this_payload++;
                        batch_item_count++; // Increment count of items in this specific batch
                    }
                }

                if (items_added_to_this_payload > 0) {
                    const char *json_payload_str = json_object_to_json_string_ext(blynk_payload_obj, JSON_C_TO_STRING_PLAIN);
                    if (json_payload_str == NULL) {
                        fprintf(stderr, "Failed to convert Blynk batch_ds payload to JSON string.\n");
                    } else {
                        MQTTClient_message pubmsg = MQTTClient_message_initializer;
                        MQTTClient_deliveryToken token;
                        pubmsg.payload = (void*)json_payload_str;
                        pubmsg.payloadlen = strlen(json_payload_str);
                        pubmsg.qos = QOS;
                        pubmsg.retained = 0;

                        if (verbose) {
                            printf("Publishing to Blynk topic '%s': %s\n", BLYNK_TOPIC, json_payload_str);
                        }
                        int rc_blynk_pub = MQTTClient_publishMessage(blynkClient, BLYNK_TOPIC, &pubmsg, &token);

                        if (rc_blynk_pub != MQTTCLIENT_SUCCESS) {
                            fprintf(stderr, "Failed to publish batch data to Blynk topic %s, rc %d\n", BLYNK_TOPIC, rc_blynk_pub);
                        } else {
                            printf("Successfully published batch data to Blynk. Length: %d, Token: %d\n", pubmsg.payloadlen, token);
                        }
                    }
                }
                
                json_object_put(blynk_payload_obj); // Free the payload object for this batch

                // If there are more rows to process, add a small delay to avoid flooding the Blynk server
                if (row_index < total_rows_to_process && items_added_to_this_payload > 0) {
                    usleep(250000); // 250ms delay
                }
            }
        } else {
            printf("Blynk client not connected. Cannot send batch data to Blynk.\n");
        }

    } else {
        // Existing logic for other messages on the main client (if any)
        // For now, assume this callback was primarily for the local broker interactions if needed,
        // or just a placeholder. The Blynk specific time window is handled by blynk_msgarrvd.
        printf("Message arrived on main client (topic: %s), not watertable JSON. No action taken by this handler for this message.\n", topicName);
        printf("          topic: %s  \n", topicName); // Redundant from above, kept for consistency with original snippet
        printf("         length: %d  \n", topicLen); // topicLen refers to topic name length
        printf("     PayloadLen: %d\n", message->payloadlen);
        // If you expect other types of messages for `client`, handle them here.
    }

    MQTTClient_freeMessage(&message);
    MQTTClient_free(topicName);
    return 1;
}

void connlost(void *context, char *cause)
{
   printf("\nConnection lost\n");
   printf("     cause: %s\n", cause);
   // This callback is for the main 'client'.
   // If it's crucial for this client to also trigger a global flag for reconnection by main,
   // a similar mechanism to blynkClient_connected_flag could be implemented.
   // For now, its loss is handled as per original logic (program might exit or try to proceed).
   connection_lost_flag = 1; // Signal main loop about connection loss
}

// New connection lost callback for blynkClient
void blynk_connlost(void *context, char *cause)
{
   MQTTClient *blynk_client_handle_ptr = (MQTTClient*)context;
   printf("\nBlynk Connection lost\n");
   printf("     cause: %s\n", cause);
   // Log a more specific message if possible, perhaps including the client ID if context allows differentiation
   //log_message("BlynkW: Connection lost. Cause: %s", cause);
   blynkClient_initialized_and_connected = 0; // Update the global flag
   // Note: We do not destroy or recreate the client handle here.
   // The main loop's reconnection logic will use initialize_blynk_client() 
   // which can attempt to recreate if the handle is NULL or use the existing one.
}

// New message arrived callback for blynkClient
int blynk_msgarrvd(void *context, char *topicName, int topicLen, MQTTClient_message *message)
{
    printf("Blynk message arrived:\n");
    printf("          topic: %s\n", topicName);
    // printf("         length: %d\n", topicLen); // topicLen is for topicName, not payload
    printf("     PayloadLen: %d\n", message->payloadlen);
    printf("        Payload: %.*s\n", message->payloadlen, (char*)message->payload);

    // Expected payload is an integer as a string (e.g., "0", "1", ...)
    if (message->payloadlen > 0) {
        char payload_str[message->payloadlen + 1];
        strncpy(payload_str, (char*)message->payload, message->payloadlen);
        payload_str[message->payloadlen] = '\0';
        
        // Robustly convert payload to integer
        char *endptr;
        long blynk_val_long = strtol(payload_str, &endptr, 10);

        // Check for conversion errors
        if (endptr == payload_str || *endptr != '\0') {
            fprintf(stderr, "Blynk_msgarrvd: Malformed integer payload received: %s\n", payload_str);
        } else {
            int blynk_value = (int)blynk_val_long; // Cast to int after successful strtol
            const char* selected_time_window = NULL;

            for (int i = 0; i < numTimeWindowEntries; i++) {
                if (timeWindowMap[i].blynkValue == blynk_value) {
                    selected_time_window = timeWindowMap[i].timeWindowString;
                    break;
                }
            }

            if (selected_time_window) {
                if (client != NULL && MQTTClient_isConnected(client)) {
                    char json_payload[128]; // Sufficient for {"range": "somestring"}
                    snprintf(json_payload, sizeof(json_payload), "{\"range\": \"%s\"}", selected_time_window);

                    printf("Blynk selected value: %d -> %s. Publishing to mwp_data_service: %s\n", 
                           blynk_value, selected_time_window, json_payload);
                    
                    MQTTClient_message pubmsg = MQTTClient_message_initializer;
                    MQTTClient_deliveryToken token;
                    pubmsg.payload = json_payload;
                    pubmsg.payloadlen = strlen(json_payload);
                    pubmsg.qos = QOS; // Assuming QOS is defined (it is)
                    pubmsg.retained = 0;
                    
                    int rc = MQTTClient_publishMessage(client, MWP_DATA_SERVICE_QUERY_TOPIC, &pubmsg, &token);
                    if (rc != MQTTCLIENT_SUCCESS) {
                        fprintf(stderr, "Blynk_msgarrvd: Failed to publish to %s, rc %d\n", MWP_DATA_SERVICE_QUERY_TOPIC, rc);
                        // log_message("BlynkW: Error == Failed to publish to mwp_data_service. RC: %d\n", rc);
                    } else {
                        printf("Blynk_msgarrvd: Successfully published to %s, token %d. Waiting for completion...\n", MWP_DATA_SERVICE_QUERY_TOPIC, token);
                        // Optional: Wait for completion, but be mindful of blocking the callback
                        // MQTTClient_waitForCompletion(client, token, TIMEOUT/2); // Shorter timeout than main publish
                    }
                } else {
                    fprintf(stderr, "Blynk_msgarrvd: Main MQTT client (for mwp_data_service) is not connected. Cannot publish.\n");
                    // log_message("BlynkW: Main MQTT client not connected. Cannot send time window.");
                }
            } else {
                fprintf(stderr, "Blynk_msgarrvd: Unknown Blynk value received: %d. No time window mapped.\n", blynk_value);
                // log_message("BlynkW: Received unknown time window value: %d", blynk_value);
            }
        }
    }

    MQTTClient_freeMessage(&message);
    MQTTClient_free(topicName);
    return 1; // Indicate success to the library
}

int loop(MQTTClient blynkClient)
{
   /* Original body of loop function commented out as requested
   MQTTClient_message pubmsg = MQTTClient_message_initializer;
   MQTTClient_deliveryToken token;
   int rc;
   char payload_str[55]; // For LED color strings, ensure size is adequate
   int i;

   const char *ledcolor[] = {"Green",
                             "Blue",
                             "Orange",
                             "Red",
                             "Yellow",
                             "Purple",
                             "Fuscia",
                             "Black"};

   const char *ledcolorPalette[] = {"0x00ff00",  // green
                                    "0x0000ff",  // blue
                                    "0xff8000",  // orange
                                    "0xff0000",  // red
                                    "0xffff00",  // yellow
                                    "0xbf00ff",  // purple
                                    "0xfe2e9a",  // fuscia
                                    "0x000000"}; // black

   // --- LED Color Publishes (retaining original behavior: no critical error check leading to client destruction here) ---
   // A more robust implementation would check return codes for these too.
   strcpy(payload_str, ledcolorPalette[(int)monitor_.monitor.Well_1_LED_Color]);
   pubmsg.payload = (void*)payload_str;
   pubmsg.payloadlen = strlen(payload_str);
   pubmsg.qos = QOS;
   pubmsg.retained = 0;
   MQTTClient_publishMessage(blynkClient, "ds/Well_1_LED_Bright/prop/color", &pubmsg, &token);
   
   strcpy(payload_str, ledcolorPalette[(int)monitor_.monitor.Well_2_LED_Color]);
   MQTTClient_publishMessage(blynkClient, "ds/Well_2_LED_Bright/prop/color", &pubmsg, &token);
   
   strcpy(payload_str, ledcolorPalette[(int)monitor_.monitor.Well_3_LED_Color]);
   MQTTClient_publishMessage(blynkClient, "ds/Well_3_LED_Bright/prop/color", &pubmsg, &token);
   
   strcpy(payload_str, ledcolorPalette[(int)monitor_.monitor.Irrig_4_LED_Color]);
   MQTTClient_publishMessage(blynkClient, "ds/Irrig_4_LED_Bright/prop/color", &pubmsg, &token);


   //***  SEND INFO TO BLYNK (Main JSON Data Publish) ***

   json_object *root = json_object_new_object();
   if (!root) {
      printf("CRITICAL: Failed to create json_object in loop(). Skipping this publish cycle.\n");
      return 0; // Indicate success for this cycle to main, but operation was skipped. Client not destroyed.
   }

   for (i=0; i<=MONITOR_LEN-13; i++) {
      json_object_object_add(root, monitor_ClientData_var_name [i], json_object_new_double(monitor_.data_payload[i]));
   }
   for (i=18; i<=MONITOR_LEN-1; i++) {
      json_object_object_add(root, monitor_ClientData_var_name [i], json_object_new_int(monitor_.data_payload[i]));
   }
   
   const char *json_string = json_object_to_json_string(root);
   pubmsg.payload = (void *)json_string;
   pubmsg.payloadlen = strlen(json_string);
   pubmsg.qos = QOS;
   pubmsg.retained = 0;
   
   rc = MQTTClient_publishMessage(blynkClient, BLYNK_TOPIC, &pubmsg, &token);
   if (rc != MQTTCLIENT_SUCCESS) {
      printf("MQTT Publish (JSON) failed to topic %s, rc %d. Destroying client in loop().\n", BLYNK_TOPIC, rc);
      json_object_put(root); 
      MQTTClient_disconnect(blynkClient, 1000); // Disconnect client (operates on the resource via handle copy)
      MQTTClient_destroy(&blynkClient);         // Destroy client resource (sets local blynkClient copy to NULL)
      return -1; // Signal critical MQTT failure to main
   }

   rc = MQTTClient_waitForCompletion(blynkClient, token, TIMEOUT);
   json_object_put(root); // Free JSON object after MQTT lib is done with payload (publish and wait)
   // root = NULL; // Good practice if root were to be used further, to prevent double free.

   if (rc != MQTTCLIENT_SUCCESS) {
      printf("MQTT WaitForCompletion (JSON) failed for token %d, topic %s, rc %d. Destroying client in loop().\n", token, BLYNK_TOPIC, rc);
      MQTTClient_disconnect(blynkClient, 1000); 
      MQTTClient_destroy(&blynkClient);        
      return -1; // Signal critical MQTT failure to main
   }
   */
   return 0; // Success for this loop iteration
}

// New function to initialize and connect the Blynk MQTT client
int initialize_blynk_client(MQTTClient* blynk_client_handle_ptr) {
    MQTTClient_connectOptions blynk_conn_opts = MQTTClient_connectOptions_initializer;
    int rc;

    // ADDRESS, CLIENTID, BLYNK_DEVICE_NAME, BLYNK_AUTH_TOKEN are #defines from the top of the file
    char blynk_actual_address[256]; // For "blynk.cloud:1883"
    // The ADDRESS define is "blynk.cloud:1883", which is not a full URI.
    // MQTTClient_create expects "tcp://hostname:port" or "ssl://hostname:port" etc.
    // Let's ensure it's correctly formatted. Assuming non-TLS for now based on port 1883.
    // It seems the original code for `client` uses `snprintf(mqtt_address, sizeof(mqtt_address), "tcp://%s:%d", mqtt_ip, mqtt_port);`
    // The ADDRESS for blynk doesn't include the scheme. Let's assume Paho handles it or it's an oversight.
    // For consistency and robustness, it should be "tcp://blynk.cloud:1883".
    // However, the original code `MQTTClient_create(blynk_client_handle_ptr, ADDRESS, ...)` seems to work,
    // so Paho might prepend "tcp://" if scheme is missing and no explicit transport options are set.
    // Will proceed with current ADDRESS define.

    // Ensure the client handle pointer is not NULL before dereferencing
    if (!blynk_client_handle_ptr) {
        fprintf(stderr, "Initialize: blynk_client_handle_ptr is NULL.\n");
        return -1; // Or some other error code indicating invalid argument
    }

    // If *blynk_client_handle_ptr is not NULL, it might mean we are trying to re-initialize
    // an existing client. Paho MQTT library might require destroying the old one first if create is called again.
    // For simplicity, let's assume if *blynk_client_handle_ptr is non-NULL, it's already created and we might be reconnecting.
    // However, the current logic in main creates it as NULL and calls this. If connection fails, main does not NULL it before retrying.
    // Safest is to destroy if non-NULL before creating again, or ensure create is only called on a NULL handle.
    // For now, this function's main purpose is initial creation and connection.
    // The main loop handles the blynkClient being NULL or not.

    // Create a unique client ID for blynk client, e.g., by appending "_blynk"
    char blynk_client_id[100];
    snprintf(blynk_client_id, sizeof(blynk_client_id), "%s_blynk", CLIENTID);

    if ((rc = MQTTClient_create(blynk_client_handle_ptr, ADDRESS, blynk_client_id, MQTTCLIENT_PERSISTENCE_NONE, NULL)) != MQTTCLIENT_SUCCESS) {
        printf("Initialize: Failed to create Blynk MQTT client, rc %d\n", rc);
        // *blynk_client_handle_ptr will be unmodified or set by Paho on failure, usually to NULL or invalid.
        return rc;
    }

    // Set callbacks for blynkClient before connecting
    // Pass the client handle itself as context, useful if the callback needs to operate on it
    // or if multiple clients were using the same callback (not the case here but good practice).
    if ((rc = MQTTClient_setCallbacks(*blynk_client_handle_ptr, (void*)*blynk_client_handle_ptr, blynk_connlost, blynk_msgarrvd, delivered)) != MQTTCLIENT_SUCCESS) {
        printf("Initialize: Failed to set callbacks for Blynk client, rc %d\n", rc);
        MQTTClient_destroy(blynk_client_handle_ptr); // Clean up the created client
        return rc;
    }

    // Set Blynk MQTT connection options
    blynk_conn_opts.username = BLYNK_DEVICE_NAME; 
    blynk_conn_opts.password = BLYNK_AUTH_TOKEN; // Use Auth Token as password
    blynk_conn_opts.keepAliveInterval = 45; // As per original blynkClient settings
    blynk_conn_opts.cleansession = 1;
    // Consider: blynk_conn_opts.automaticReconnect = 1; // If Paho lib supports & desired over manual loop

    printf("Initialize: Connecting to Blynk MQTT server at %s...\n", ADDRESS);
    if ((rc = MQTTClient_connect(*blynk_client_handle_ptr, &blynk_conn_opts)) != MQTTCLIENT_SUCCESS) {
        printf("Initialize: Failed to connect to Blynk MQTT server, rc %d\n", rc);
        MQTTClient_destroy(blynk_client_handle_ptr); // Clean up client if connection failed
        return rc;
    }

    printf("Initialize: Successfully connected to Blynk MQTT server: %s\n", ADDRESS);

    // Subscribe to the Blynk V0 downlink topic
    char blynk_v0_topic[256];
    // snprintf(blynk_v0_topic, sizeof(blynk_v0_topic), BLYNK_DOWNLINK_V0_TOPIC_FORMAT, BLYNK_AUTH_TOKEN); // Old way
    // snprintf(blynk_v0_topic, sizeof(blynk_v0_topic), "%s", BLYNK_V0_DOWNLINK_DS_TOPIC); // Previous way
    snprintf(blynk_v0_topic, sizeof(blynk_v0_topic), "%s", BLYNK_TIMEWINDOW_DOWNLINK_DS_TOPIC); // Current correct way
    
    printf("Initialize: Subscribing Blynk client to %s with QoS %d\n", blynk_v0_topic, QOS);
    if ((rc = MQTTClient_subscribe(*blynk_client_handle_ptr, blynk_v0_topic, QOS)) != MQTTCLIENT_SUCCESS) {
        printf("Initialize: Failed to subscribe Blynk client to %s, rc %d\n", blynk_v0_topic, rc);
        //log_message("BlynkW: Error == Failed to subscribe to %s. RC: %d\n", blynk_v0_topic, rc);
        MQTTClient_disconnect(*blynk_client_handle_ptr, 1000); // Disconnect first
        MQTTClient_destroy(blynk_client_handle_ptr);         // Then destroy
        return rc;
    } else {
        printf("Initialize: Successfully subscribed Blynk client to %s\n", blynk_v0_topic);
        //log_message("BlynkW: Subscribed to %s successfully.", blynk_v0_topic);
    }

    return MQTTCLIENT_SUCCESS;
}

int main(int argc, char *argv[])
{
   // Set the global double-to-string format for all json-c operations in this program.
   // "%.4g" formats numbers to 4 significant digits.
   json_c_set_serialization_double_format("%.4g", 0);

   // MQTTClient client; // Moved to file scope
   MQTTClient_connectOptions conn_opts = MQTTClient_connectOptions_initializer;
   int rc;
   int opt;
   const char *mqtt_ip = NULL;
   int mqtt_port = 0;

   while ((opt = getopt(argc, argv, "vPD")) != -1) {
      switch (opt) {
         case 'v':
               verbose = TRUE;
               break;
         case 'P':
               mqtt_ip = PROD_MQTT_IP;
               mqtt_port = PROD_MQTT_PORT;
               break;
         case 'D':
               mqtt_ip = DEV_MQTT_IP;
               mqtt_port = DEV_MQTT_PORT;
               break;
         default:
               fprintf(stderr, "Usage: %s [-v] [-P | -D]\n", argv[0]);
               return 1;
      }
   }

   if (verbose) {
      printf("Verbose mode enabled\n");
   }

   if (mqtt_ip == NULL) {
      fprintf(stderr, "Please specify either Production (-P) or Development (-D) server\n");
      return 1;
   }

   char mqtt_address[256];
   snprintf(mqtt_address, sizeof(mqtt_address), "tcp://%s:%d", mqtt_ip, mqtt_port);

   printf("MQTT Address: %s\n", mqtt_address);
   
   //log_message("Blynk: Started\n");

   if ((rc = MQTTClient_create(&client, mqtt_address, CLIENTID,
                               MQTTCLIENT_PERSISTENCE_NONE, NULL)) != MQTTCLIENT_SUCCESS)
   {
      printf("Failed to create client, return code %d\n", rc);
      //log_message("Blynk: Error == Failed to Create Client. Return Code: %d\n", rc);
      rc = EXIT_FAILURE;
      exit(EXIT_FAILURE);
   }
   
   if ((rc = MQTTClient_setCallbacks(client, NULL, connlost, msgarrvd, delivered)) != MQTTCLIENT_SUCCESS)
   {
      printf("Failed to set callbacks, return code %d\n", rc);
      //log_message("Blynk: Error == Failed to Set Callbacks. Return Code: %d\n", rc);
      rc = EXIT_FAILURE;
      exit(EXIT_FAILURE);
   }
   
   conn_opts.keepAliveInterval = 120;
   conn_opts.cleansession = 1;
   //conn_opts.username = mqttUser;       //only if req'd by MQTT Server
   //conn_opts.password = mqttPassword;   //only if req'd by MQTT Server
   if ((rc = MQTTClient_connect(client, &conn_opts)) != MQTTCLIENT_SUCCESS)
   {
      printf("Failed to connect main client, return code %d\n", rc);
      //log_message("Blynk: Error == Failed to Connect. Return Code: %d\n", rc);
      // rc = EXIT_FAILURE; // Original code did not set rc here for exit
      MQTTClient_destroy(&client); // Ensure client is destroyed if connect fails
      exit(EXIT_FAILURE);
   }

   // Subscribe the main client to the watertable JSON topic
   printf("Main: Subscribing main client to watertable JSON topic: %s (QoS: %d)\n", MWP_WATERTABLE_JSON_TOPIC, QOS);
   if ((rc = MQTTClient_subscribe(client, MWP_WATERTABLE_JSON_TOPIC, QOS)) != MQTTCLIENT_SUCCESS) {
      printf("Main: Failed to subscribe main client to %s, rc %d\n", MWP_WATERTABLE_JSON_TOPIC, rc);
      //log_message("BlynkW: Error == Failed to subscribe to %s. RC: %d\n", MWP_WATERTABLE_JSON_TOPIC, rc);
      // Depending on how critical this subscription is, you might want to exit or handle differently.
      // For now, we'll print error and continue, but table data from mwp_data_service won't be received.
   } else {
      printf("Main: Successfully subscribed main client to %s\n", MWP_WATERTABLE_JSON_TOPIC);
      //log_message("BlynkW: Subscribed to %s successfully.", MWP_WATERTABLE_JSON_TOPIC);
   }

   // --- Blynk Client (blynkClient) Setup and Management --- 
   // MQTTClient blynkClient = NULL; // Moved to file scope
   int rc_loop_status; // To store return status from new loop()

   // Initial attempt to connect the Blynk client
   printf("Main: Attempting initial Blynk client connection...\n");
   if (initialize_blynk_client(&blynkClient) == MQTTCLIENT_SUCCESS) {
      blynkClient_initialized_and_connected = 1;
      // log_message("BlynkW: Initial Blynk client connection successful."); // Optional logging
   } else {
      printf("Main: Initial Blynk client connection failed. Will retry in main loop.\n");
      // initialize_blynk_client handles cleanup of blynkClient on its own failure, so blynkClient should be NULL or safe
      // log_message("BlynkW: Initial Blynk client connection failed."); // Optional logging
   }
   
   // Subscribe the main client (for mwp/data/monitor/#)
   //printf("Main: Subscribing main client to all monitor topics: %s\nfor client: %s using QoS: %d\n\n", "mwp/data/monitor/#", MONITOR_CLIENTID, QOS);
   // log_message("BlynkW: Subscribing to topic: %s for client: %s\n", "mwp/data/monitor/#", MONITOR_CLIENTID);
   //MQTTClient_subscribe(client, "mwp/data/monitor/#", QOS); // Assuming QOS and MONITOR_CLIENTID are defined

   // --- Main Application Loop ---
   while (TRUE) // Replace TRUE with a proper shutdown condition if needed
   {
      // --- Handle Blynk Client State ---
      if (!blynkClient_initialized_and_connected) {
         printf("Main: Blynk client not connected. Attempting to reconnect in %d seconds...\n", RECONNECT_DELAY_SECONDS);
         // log_message("BlynkW: Attempting to reconnect Blynk client."); // Optional
         sleep(RECONNECT_DELAY_SECONDS); 
         if (initialize_blynk_client(&blynkClient) == MQTTCLIENT_SUCCESS) {
            blynkClient_initialized_and_connected = 1;
            printf("Main: Blynk client reconnected successfully.\n");
            // log_message("BlynkW: Reconnected Blynk client successfully."); // Optional
         } else {
            printf("Main: Blynk client reconnection failed. Will retry later.\n");
            // log_message("BlynkW: Blynk client reconnection failed."); // Optional
            // blynkClient should be NULL or properly managed by initialize_blynk_client upon its failure.
         }
      }

      // If Blynk client is ready, call its processing loop
      if (blynkClient_initialized_and_connected) {
         if (blynkClient == NULL) { // Safety check, should not happen if logic is correct
             printf("CRITICAL ERROR in Main: blynkClient_initialized_and_connected is true, but blynkClient handle is NULL! Correcting state.\n");
             blynkClient_initialized_and_connected = 0; 
         } else {
            rc_loop_status = loop(blynkClient); // Call the modified loop function
            if (rc_loop_status == -1) {
               printf("Main: loop() reported critical MQTT error for Blynk. Client resource was destroyed by loop.\n");
               // log_message("BlynkW: Critical error in Blynk client loop. Client destroyed by loop."); //Optional
               blynkClient = NULL; // The handle 'blynkClient' in main is now stale. Mark it NULL.
                                  // The resource was destroyed by loop(), do not destroy again here.
               blynkClient_initialized_and_connected = 0; 
            } 
            // else if (rc_loop_status == 0) { /* Loop was successful or skipped harmlessly */ }
         }
      } else {
         printf("Main: Skipping Blynk operations as client is not connected.\n");
      }

      // TODO: Add logic for the main 'client' (mwp/data/monitor/#) if it needs yielding or periodic checks.
      // For example, MQTTClient_yield() if 'client' is asynchronous or uses persistence that needs it.
      // The original code structure implies 'client' messages are handled via callbacks (msgarrvd).

      sleep(1); // Main application cycle delay
   }

   //log_message("Blynk: Exited Main Loop\n"); // This seems to refer to the overall application
   
   // --- Cleanup before exit ---
   printf("Main: Cleaning up resources before exit...\n");

   // Cleanup for the first 'client' (mwp/data/monitor/#)
   if (client != NULL) { // Check if client was successfully created
      printf("Main: Unsubscribing and disconnecting main client.\n");
      MQTTClient_unsubscribe(client, MWP_WATERTABLE_JSON_TOPIC); // Unsubscribe from the watertable topic
      // MQTTClient_unsubscribe(client, "mwp/data/monitor/#"); // If this was ever subscribed to
      MQTTClient_disconnect(client, 10000);
      MQTTClient_destroy(&client);
   }

   // Cleanup for blynkClient if it exists and is connected/initialized
   if (blynkClient_initialized_and_connected && blynkClient != NULL) {
      printf("Main: Disconnecting and destroying Blynk client before exit.\n");
      MQTTClient_disconnect(blynkClient, 10000);
      MQTTClient_destroy(&blynkClient);
   } else if (blynkClient != NULL) { // If not connected but handle isn't NULL (e.g. creation failed mid-way outside init func)
      printf("Main: Destroying non-connected Blynk client before exit.\n");
      MQTTClient_destroy(&blynkClient);
   }

   printf("Main: Application exiting.\n");
   //log_message("Blynk: Exited Main Loop\n"); // Duplicate? Or different context?
   return EXIT_SUCCESS;
}