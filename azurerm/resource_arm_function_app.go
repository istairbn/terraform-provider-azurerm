package azurerm

import (
	"fmt"
	"log"

	"github.com/Azure/azure-sdk-for-go/services/web/mgmt/2016-09-01/web"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/helper/validation"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

// Azure Function App shares the same infrastructure with Azure App Service.
// So this resource will reuse most of the App Service code, but remove the configurations which are not applicable for Function App.
func resourceArmFunctionApp() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmFunctionAppCreate,
		Read:   resourceArmFunctionAppRead,
		Update: resourceArmFunctionAppUpdate,
		Delete: resourceArmFunctionAppDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validateAppServiceName,
			},

			"resource_group_name": resourceGroupNameSchema(),

			"location": locationSchema(),

			"app_service_plan_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"enabled": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  true,

				// TODO: (tombuildsstuff) support Update once the API is fixed:
				// https://github.com/Azure/azure-rest-api-specs/issues/1697
				ForceNew: true,
			},

			"version": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "~1",
				ValidateFunc: validation.StringInSlice([]string{
					"~1",
					"beta",
				}, false),
			},

			"storage_connection_string": {
				Type:      schema.TypeString,
				Required:  true,
				ForceNew:  true,
				Sensitive: true,
			},

			"app_settings": {
				Type:     schema.TypeMap,
				Optional: true,
			},

			"connection_string": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"value": {
							Type:     schema.TypeString,
							Required: true,
						},
						"type": {
							Type:     schema.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								string(web.APIHub),
								string(web.Custom),
								string(web.DocDb),
								string(web.EventHub),
								string(web.MySQL),
								string(web.NotificationHub),
								string(web.PostgreSQL),
								string(web.RedisCache),
								string(web.ServiceBus),
								string(web.SQLAzure),
								string(web.SQLServer),
							}, true),
							DiffSuppressFunc: ignoreCaseDiffSuppressFunc,
						},
					},
				},
			},

			// TODO: (tombuildsstuff) support Update once the API is fixed:
			// https://github.com/Azure/azure-rest-api-specs/issues/1697
			"tags": tagsForceNewSchema(),

			"default_hostname": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"outbound_ip_addresses": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"client_affinity_enabled": {
				Type:     schema.TypeBool,
				Optional: true,
				Computed: true,

				// TODO: (tombuildsstuff) support Update once the API is fixed:
				// https://github.com/Azure/azure-rest-api-specs/issues/1697
				ForceNew: true,
			},

			"https_only": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,

				// TODO: (tombuildsstuff) support Update once the API is fixed:
				// https://github.com/Azure/azure-rest-api-specs/issues/1697
				ForceNew: true,
			},

			"site_config": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"always_on": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  false,
						},
						"use_32_bit_worker_process": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  true,
						},
						"websockets_enabled": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  false,
						},
					},
				},
			},
		},
	}
}

func resourceArmFunctionAppCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).appServicesClient
	ctx := meta.(*ArmClient).StopContext

	log.Printf("[INFO] preparing arguments for AzureRM Function App creation.")

	name := d.Get("name").(string)

	availabilityRequest := web.ResourceNameAvailabilityRequest{
		Name: utils.String(name),
		Type: web.CheckNameResourceTypesMicrosoftWebsites,
	}
	available, err := client.CheckNameAvailability(ctx, availabilityRequest)
	if err != nil {
		return fmt.Errorf("Error checking if the name %q was available: %+v", name, err)
	}

	if !*available.NameAvailable {
		return fmt.Errorf("The name %q used for the Function App needs to be globally unique and isn't available: %s", name, *available.Message)
	}

	resGroup := d.Get("resource_group_name").(string)
	location := azureRMNormalizeLocation(d.Get("location").(string))
	kind := "functionapp"
	appServicePlanID := d.Get("app_service_plan_id").(string)
	enabled := d.Get("enabled").(bool)
	clientAffinityEnabled := d.Get("client_affinity_enabled").(bool)
	tags := d.Get("tags").(map[string]interface{})
	basicAppSettings := getBasicFunctionAppAppSettings(d)
	siteConfig := expandFunctionAppSiteConfig(d)
	siteConfig.AppSettings = &basicAppSettings

	siteEnvelope := web.Site{
		Kind:     &kind,
		Location: &location,
		Tags:     expandTags(tags),
		SiteProperties: &web.SiteProperties{
			ServerFarmID:          utils.String(appServicePlanID),
			Enabled:               utils.Bool(enabled),
			ClientAffinityEnabled: utils.Bool(clientAffinityEnabled),
			SiteConfig:            &siteConfig,
		},
	}

	createFuture, err := client.CreateOrUpdate(ctx, resGroup, name, siteEnvelope)
	if err != nil {
		return err
	}

	err = createFuture.WaitForCompletion(ctx, client.Client)
	if err != nil {
		return err
	}

	read, err := client.Get(ctx, resGroup, name)
	if err != nil {
		return err
	}
	if read.ID == nil {
		return fmt.Errorf("Cannot read Function App %s (resource group %s) ID", name, resGroup)
	}

	d.SetId(*read.ID)

	return resourceArmFunctionAppUpdate(d, meta)
}

func resourceArmFunctionAppUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).appServicesClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resGroup := id.ResourceGroup
	name := id.Path["sites"]

	if d.HasChange("app_settings") || d.HasChange("version") {
		appSettings := expandFunctionAppAppSettings(d)
		settings := web.StringDictionary{
			Properties: appSettings,
		}

		_, err := client.UpdateApplicationSettings(ctx, resGroup, name, settings)
		if err != nil {
			return fmt.Errorf("Error updating Application Settings for Function App %q: %+v", name, err)
		}
	}

	if d.HasChange("site_config") {
		siteConfig := expandFunctionAppSiteConfig(d)
		siteConfigResource := web.SiteConfigResource{
			SiteConfig: &siteConfig,
		}
		_, err := client.CreateOrUpdateConfiguration(ctx, resGroup, name, siteConfigResource)
		if err != nil {
			return fmt.Errorf("Error updating Configuration for Function App %q: %+v", name, err)
		}
	}

	if d.HasChange("connection_string") {
		// update the ConnectionStrings
		connectionStrings := expandFunctionAppConnectionStrings(d)
		properties := web.ConnectionStringDictionary{
			Properties: connectionStrings,
		}

		_, err := client.UpdateConnectionStrings(ctx, resGroup, name, properties)
		if err != nil {
			return fmt.Errorf("Error updating Connection Strings for App Service %q: %+v", name, err)
		}
	}

	return resourceArmFunctionAppRead(d, meta)
}

func resourceArmFunctionAppRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).appServicesClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resGroup := id.ResourceGroup
	name := id.Path["sites"]

	resp, err := client.Get(ctx, resGroup, name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			log.Printf("[DEBUG] Function App %q (resource group %q) was not found - removing from state", name, resGroup)
			d.SetId("")
			return nil
		}
		return fmt.Errorf("Error making Read request on AzureRM Function App %q: %+v", name, err)
	}

	appSettingsResp, err := client.ListApplicationSettings(ctx, resGroup, name)
	if err != nil {
		return fmt.Errorf("Error making Read request on AzureRM Function App AppSettings %q: %+v", name, err)
	}

	connectionStringsResp, err := client.ListConnectionStrings(ctx, resGroup, name)
	if err != nil {
		return fmt.Errorf("Error making Read request on AzureRM Function App ConnectionStrings %q: %+v", name, err)
	}

	d.Set("name", name)
	d.Set("resource_group_name", resGroup)
	if location := resp.Location; location != nil {
		d.Set("location", azureRMNormalizeLocation(*location))
	}

	if props := resp.SiteProperties; props != nil {
		d.Set("app_service_plan_id", props.ServerFarmID)
		d.Set("enabled", props.Enabled)
		d.Set("default_hostname", props.DefaultHostName)
		d.Set("https_only", props.HTTPSOnly)
		d.Set("outbound_ip_addresses", props.OutboundIPAddresses)
		d.Set("client_affinity_enabled", props.ClientAffinityEnabled)
	}

	appSettings := flattenAppServiceAppSettings(appSettingsResp.Properties)

	d.Set("storage_connection_string", appSettings["AzureWebJobsStorage"])
	d.Set("version", appSettings["FUNCTIONS_EXTENSION_VERSION"])

	delete(appSettings, "AzureWebJobsDashboard")
	delete(appSettings, "AzureWebJobsStorage")
	delete(appSettings, "FUNCTIONS_EXTENSION_VERSION")
	delete(appSettings, "WEBSITE_CONTENTSHARE")
	delete(appSettings, "WEBSITE_CONTENTAZUREFILECONNECTIONSTRING")

	if err := d.Set("app_settings", appSettings); err != nil {
		return err
	}
	if err := d.Set("connection_string", flattenFunctionAppConnectionStrings(connectionStringsResp.Properties)); err != nil {
		return err
	}

	configResp, err := client.GetConfiguration(ctx, resGroup, name)
	if err != nil {
		return fmt.Errorf("Error making Read request on AzureRM Function App Configuration %q: %+v", name, err)
	}

	siteConfig := flattenFunctionAppSiteConfig(configResp.SiteConfig)
	if err := d.Set("site_config", siteConfig); err != nil {
		return err
	}

	flattenAndSetTags(d, resp.Tags)

	return nil
}

func resourceArmFunctionAppDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).appServicesClient

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resGroup := id.ResourceGroup
	name := id.Path["sites"]

	log.Printf("[DEBUG] Deleting Function App %q (resource group %q)", name, resGroup)

	deleteMetrics := true
	deleteEmptyServerFarm := false
	ctx := meta.(*ArmClient).StopContext
	resp, err := client.Delete(ctx, resGroup, name, &deleteMetrics, &deleteEmptyServerFarm)
	if err != nil {
		if !utils.ResponseWasNotFound(resp) {
			return err
		}
	}

	return nil
}

func getBasicFunctionAppAppSettings(d *schema.ResourceData) []web.NameValuePair {
	dashboardPropName := "AzureWebJobsDashboard"
	storagePropName := "AzureWebJobsStorage"
	functionVersionPropName := "FUNCTIONS_EXTENSION_VERSION"
	contentSharePropName := "WEBSITE_CONTENTSHARE"
	contentFileConnStringPropName := "WEBSITE_CONTENTAZUREFILECONNECTIONSTRING"

	storageConnection := d.Get("storage_connection_string").(string)
	functionVersion := d.Get("version").(string)
	contentShare := d.Get("name").(string) + "-content"

	return []web.NameValuePair{
		{Name: &dashboardPropName, Value: &storageConnection},
		{Name: &storagePropName, Value: &storageConnection},
		{Name: &functionVersionPropName, Value: &functionVersion},
		{Name: &contentSharePropName, Value: &contentShare},
		{Name: &contentFileConnStringPropName, Value: &storageConnection},
	}
}

func expandFunctionAppAppSettings(d *schema.ResourceData) map[string]*string {
	output := expandAppServiceAppSettings(d)

	basicAppSettings := getBasicFunctionAppAppSettings(d)
	for _, p := range basicAppSettings {
		output[*p.Name] = p.Value
	}

	return output
}

func expandFunctionAppSiteConfig(d *schema.ResourceData) web.SiteConfig {
	configs := d.Get("site_config").([]interface{})
	siteConfig := web.SiteConfig{}

	if len(configs) == 0 {
		return siteConfig
	}

	config := configs[0].(map[string]interface{})

	if v, ok := config["always_on"]; ok {
		siteConfig.AlwaysOn = utils.Bool(v.(bool))
	}

	if v, ok := config["use_32_bit_worker_process"]; ok {
		siteConfig.Use32BitWorkerProcess = utils.Bool(v.(bool))
	}

	if v, ok := config["websockets_enabled"]; ok {
		siteConfig.WebSocketsEnabled = utils.Bool(v.(bool))
	}

	return siteConfig
}

func flattenFunctionAppSiteConfig(input *web.SiteConfig) []interface{} {
	results := make([]interface{}, 0)
	result := make(map[string]interface{}, 0)

	if input == nil {
		log.Printf("[DEBUG] SiteConfig is nil")
		return results
	}

	if input.AlwaysOn != nil {
		result["always_on"] = *input.AlwaysOn
	}

	if input.Use32BitWorkerProcess != nil {
		result["use_32_bit_worker_process"] = *input.Use32BitWorkerProcess
	}

	if input.WebSocketsEnabled != nil {
		result["websockets_enabled"] = *input.WebSocketsEnabled
	}

	results = append(results, result)
	return results
}

func expandFunctionAppConnectionStrings(d *schema.ResourceData) map[string]*web.ConnStringValueTypePair {
	input := d.Get("connection_string").([]interface{})
	output := make(map[string]*web.ConnStringValueTypePair, len(input))

	for _, v := range input {
		vals := v.(map[string]interface{})

		csName := vals["name"].(string)
		csType := vals["type"].(string)
		csValue := vals["value"].(string)

		output[csName] = &web.ConnStringValueTypePair{
			Value: utils.String(csValue),
			Type:  web.ConnectionStringType(csType),
		}
	}

	return output
}

func flattenFunctionAppConnectionStrings(input map[string]*web.ConnStringValueTypePair) interface{} {
	results := make([]interface{}, 0)

	for k, v := range input {
		result := make(map[string]interface{}, 0)
		result["name"] = k
		result["type"] = string(v.Type)
		result["value"] = *v.Value
		results = append(results, result)
	}

	return results
}
