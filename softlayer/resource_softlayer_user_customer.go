package softlayer

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	"github.ibm.com/riethm/gopherlayer.git/datatypes"
	"github.ibm.com/riethm/gopherlayer.git/services"
	"github.ibm.com/riethm/gopherlayer.git/session"
	"github.ibm.com/riethm/gopherlayer.git/sl"
)

func resourceSoftLayerUserCustomer() *schema.Resource {
	return &schema.Resource{
		Create:   resourceSoftLayerUserCustomerCreate,
		Read:     resourceSoftLayerUserCustomerRead,
		Update:   resourceSoftLayerUserCustomerUpdate,
		Delete:   resourceSoftLayerUserCustomerDelete,
		Exists:   resourceSoftLayerUserCustomerExists,
		Importer: &schema.ResourceImporter{},

		Schema: map[string]*schema.Schema{
			"id": &schema.Schema{
				Type:     schema.TypeInt,
				Computed: true,
			},

			"username": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"first_name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},

			"last_name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},

			"email": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"company_name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"address1": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"address2": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"city": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"state": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"country": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			// TODO Support more intuitive string values for timezone and user_status
			"timezone": &schema.Schema{
				Type:     schema.TypeInt,
				Required: true,
			},
			"user_status": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				// Active by default
				Default: 1001,
			},
			"password": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				StateFunc: func(v interface{}) string {
					hash := sha1.Sum([]byte(v.(string)))
					return hex.EncodeToString(hash[:])
				},
			},
			"permissions": &schema.Schema{
				Type:     schema.TypeSet,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set:      schema.HashString,
			},
			"has_api_key": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"api_key": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
		},
	}
}

// Create a SoftLayer_User_Customer_CustomerPermission_Permission object from the given string input
func makePermission(p string) datatypes.User_Customer_CustomerPermission_Permission {
	return datatypes.User_Customer_CustomerPermission_Permission{
		KeyName: &p,
	}
}

// Convert a "set" of permission strings to a list of SoftLayer_User_Customer_CustomerPermission_Permissions
func getPermissions(d *schema.ResourceData) []datatypes.User_Customer_CustomerPermission_Permission {
	permissionsSet := d.Get("permissions").(*schema.Set)

	if permissionsSet.Len() == 0 {
		return nil
	}

	permissions := make([]datatypes.User_Customer_CustomerPermission_Permission, 0, permissionsSet.Len())
	for _, elem := range permissionsSet.List() {
		permission := makePermission(elem.(string))

		permissions = append(permissions, permission)
	}
	return permissions
}

func resourceSoftLayerUserCustomerCreate(d *schema.ResourceData, meta interface{}) error {
	service := services.GetUserCustomerService(meta.(*session.Session))

	// Build up our creation options
	opts := datatypes.User_Customer{
		Username:     sl.String(d.Get("username").(string)),
		FirstName:    sl.String(d.Get("first_name").(string)),
		LastName:     sl.String(d.Get("last_name").(string)),
		Email:        sl.String(d.Get("email").(string)),
		CompanyName:  sl.String(d.Get("company_name").(string)),
		Address1:     sl.String(d.Get("address1").(string)),
		City:         sl.String(d.Get("city").(string)),
		State:        sl.String(d.Get("state").(string)),
		Country:      sl.String(d.Get("country").(string)),
		TimezoneId:   sl.Int(d.Get("timezone").(int)),
		UserStatusId: sl.Int(d.Get("user_status").(int)),
	}

	if address2, ok := d.GetOk("address2"); ok {
		opts.Address2 = sl.String(address2.(string))
	}

	password := d.Get("password").(string)

	res, err := service.CreateObject(&opts, &password, nil)
	if err != nil {
		return fmt.Errorf("Error creating SoftLayer User: %s", err)
	}

	d.SetId(strconv.Itoa(*res.Id))
	log.Printf("[INFO] SoftLayer User: %d", *res.Id)

	permissions := getPermissions(d)

	defaultPortalPermissions := []datatypes.User_Customer_CustomerPermission_Permission{
		{KeyName: sl.String("ACCESS_ALL_GUEST")},
		{KeyName: sl.String("ACCESS_ALL_HARDWARE")},
	}

	log.Printf("Replacing default portal permissions assigned by SoftLayer with those specified in config")

	// Set the instance ID for the service to act on
	service = service.Id(*res.Id)

	_, err = service.RemoveBulkPortalPermission(defaultPortalPermissions)
	if err != nil {
		return fmt.Errorf("Error removing default portal permissions for SoftLayer User: %s", err)
	}

	_, err = service.AddBulkPortalPermission(permissions)
	if err != nil {
		return fmt.Errorf("Error setting portal permissions for SoftLayer User: %s", err)
	}

	create_api_key_flag := d.Get("has_api_key").(bool)
	if create_api_key_flag {
		// We have to create the API key only if the flag is true. If 'false' we do not
		// take the delete action on the API key, as this is the create new user method,
		// and not the edit method.
		_, err = service.AddApiAuthenticationKey()
		if err != nil {
			return fmt.Errorf("Error creating API key: %s", err)
		}
	}

	return resourceSoftLayerUserCustomerRead(d, meta)
}

func resourceSoftLayerUserCustomerRead(d *schema.ResourceData, meta interface{}) error {
	service := services.GetUserCustomerService(meta.(*session.Session))

	userID, _ := strconv.Atoi(d.Id())

	mask := strings.Join([]string{
		"id",
		"username",
		"email",
		"firstName",
		"lastName",
		"companyName",
		"address1",
		"address2",
		"city",
		"state",
		"country",
		"timezoneId",
		"userStatusId",
		"permissions.keyName",
		"apiAuthenticationKeys.authenticationKey",
	}, ";")

	sluserObj, err := service.Id(userID).Mask(mask).GetObject()
	if err != nil {
		// If the key is somehow already destroyed, mark as
		// successfully gone
		if strings.Contains(err.Error(), "404 Not Found") {
			d.SetId("")
			return nil
		}

		return fmt.Errorf("Error retrieving SoftLayer User: %s", err)
	}

	d.Set("id", sluserObj.Id)
	d.Set("username", sluserObj.Username)
	d.Set("email", sluserObj.Email)
	d.Set("first_name", sluserObj.FirstName)
	d.Set("last_name", sluserObj.LastName)
	d.Set("company_name", sluserObj.CompanyName)
	d.Set("address1", sluserObj.Address1)
	d.Set("address2", sluserObj.Address2)
	d.Set("city", sluserObj.City)
	d.Set("state", sluserObj.State)
	d.Set("country", sluserObj.Country)
	d.Set("timezone", sluserObj.TimezoneId)
	d.Set("user_status", sluserObj.UserStatusId)

	permissions := make([]string, 0, len(sluserObj.Permissions))
	for _, elem := range sluserObj.Permissions {
		permissions = append(permissions, *elem.KeyName)
	}
	d.Set("permissions", permissions)

	// If present, extract the api key from the SoftLayer response and set the field in the resource
	if len(sluserObj.ApiAuthenticationKeys) > 0 {
		d.Set("api_key", sluserObj.ApiAuthenticationKeys[0].AuthenticationKey) // as its a computed field
		d.Set("has_api_key", true)
	} else {
		d.Set("api_key", "")
		d.Set("has_api_key", false)
	}

	return nil
}

func resourceSoftLayerUserCustomerUpdate(d *schema.ResourceData, meta interface{}) error {
	service := services.GetUserCustomerService(meta.(*session.Session))

	sluid, _ := strconv.Atoi(d.Id())

	mask := strings.Join([]string{
		"id",
		"username",
		"email",
		"firstName",
		"lastName",
		"companyName",
		"address1",
		"address2",
		"city",
		"state",
		"country",
		"timezoneId",
		"userStatusId",
		"permissions.keyName",
		"apiAuthenticationKeys.authenticationKey",
		"apiAuthenticationKeys.id",
	}, ";")

	service = service.Id(sluid)
	userObj, err := service.Mask(mask).GetObject()

	// Some fields cannot be updated such as username. Computed fields also cannot be updated
	// by explicitly providing a value. So only update the fields that are editable.
	// TODO: For now you may not update the password.
	if d.HasChange("first_name") {
		userObj.FirstName = sl.String(d.Get("first_name").(string))
	}
	if d.HasChange("last_name") {
		userObj.LastName = sl.String(d.Get("last_name").(string))
	}
	if d.HasChange("email") {
		userObj.Email = sl.String(d.Get("email").(string))
	}
	if d.HasChange("company_name") {
		userObj.CompanyName = sl.String(d.Get("company_name").(string))
	}
	if d.HasChange("address1") {
		userObj.Address1 = sl.String(d.Get("address1").(string))
	}
	if d.HasChange("address2") {
		userObj.Address2 = sl.String(d.Get("address2").(string))
	}
	if d.HasChange("city") {
		userObj.City = sl.String(d.Get("city").(string))
	}
	if d.HasChange("state") {
		userObj.State = sl.String(d.Get("state").(string))
	}
	if d.HasChange("country") {
		userObj.Country = sl.String(d.Get("country").(string))
	}
	if d.HasChange("timezone") {
		userObj.TimezoneId = sl.Int(d.Get("timezone").(int))
	}
	if d.HasChange("user_status") {
		userObj.UserStatusId = sl.Int(d.Get("user_status").(int))
	}

	_, err = service.EditObject(&userObj)
	if err != nil {
		return fmt.Errorf("Error received while editing softlayer_user: %s", err)
	}

	if d.HasChange("permissions") {
		// TODO Use set math functions (in schema.Set) to compute the difference, vs clearing and re-adding permissions
		old, new := d.GetChange("permissions")

		oldPermissions := make([]datatypes.User_Customer_CustomerPermission_Permission, 0, old.(*schema.Set).Len())
		newPermissions := make([]datatypes.User_Customer_CustomerPermission_Permission, 0, new.(*schema.Set).Len())

		for _, elem := range old.(*schema.Set).List() {
			oldPermissions = append(oldPermissions, makePermission(elem.(string)))
		}

		for _, elem := range new.(*schema.Set).List() {
			newPermissions = append(newPermissions, makePermission(elem.(string)))
		}

		// 'remove' all old permissions
		_, err = service.RemoveBulkPortalPermission(oldPermissions)
		if err != nil {
			return fmt.Errorf("Error received while removing old permissions from softlayer_user: %s", err)
		}

		// 'add' new permission set
		_, err = service.AddBulkPortalPermission(newPermissions)
		if err != nil {
			return fmt.Errorf("Error received while assigning new permissions to softlayer_user: %s", err)
		}
	}

	if d.HasChange("has_api_key") {
		// if true, then it means create an api key if none exists. Its a no-op if an api key already exists.
		// else false means, delete the api key if one exists. Its a no-op if no api key exists.
		api_key_flag := d.Get("has_api_key").(bool)

		// Get the current keys.
		keys := userObj.ApiAuthenticationKeys

		// Create a key if flag is true, and a key does not already exist.
		if api_key_flag {
			if len(keys) == 0 { // means key does not exist, so create one.
				key, err := service.AddApiAuthenticationKey()
				if err != nil {
					return fmt.Errorf("Error creating API key while editing softlayer_user resource: %s", err)
				}

				d.Set("api_key", key)
			} else {
				d.Set("api_key", keys[0].AuthenticationKey) // as api_key is a computed field
			}
		} else {
			// If false, then delete the key if there was one.
			if len(keys) > 0 {
				_, err = service.RemoveApiAuthenticationKey(keys[0].Id)
				if err != nil {
					return fmt.Errorf("Error deleting API key while editing softlayer_user resource: %s", err)
				}
			}
			d.Set("api_key", nil)
		}
	}
	return nil
}

func resourceSoftLayerUserCustomerDelete(d *schema.ResourceData, meta interface{}) error {
	service := services.GetUserCustomerService(meta.(*session.Session))

	id, _ := strconv.Atoi(d.Id())

	user := datatypes.User_Customer{
		UserStatusId: sl.Int(1021),
	}

	log.Printf("[INFO] Deleting SoftLayer user: %d", id)
	_, err := service.Id(id).EditObject(&user)
	if err != nil {
		return fmt.Errorf("Error deleting SoftLayer user: %s", err)
	}

	d.SetId("")
	return nil
}

func resourceSoftLayerUserCustomerExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	service := services.GetUserCustomerService(meta.(*session.Session))

	id, err := strconv.Atoi(d.Id())

	result, err := service.Id(id).GetObject()
	log.Println(*result.Id)

	return *result.Id == id && err == nil, nil
}