package projects

import (
	log "github.com/Sirupsen/logrus"
	"github.com/ansible-semaphore/semaphore/api/helpers"
	"github.com/ansible-semaphore/semaphore/db"
	"net/http"

	"github.com/gorilla/context"
)

// KeyMiddleware ensures a key exists and loads it to the context
func KeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		project := context.Get(r, "project").(db.Project)
		keyID, err := helpers.GetIntParam("key_id", w, r)
		if err != nil {
			return
		}

		key, err := helpers.Store(r).GetAccessKey(project.ID, keyID)

		if err != nil {
			helpers.WriteError(w, err)
			return
		}

		context.Set(r, "accessKey", key)
		next.ServeHTTP(w, r)
	})
}

// GetKeys retrieves sorted keys from the database
func GetKeys(w http.ResponseWriter, r *http.Request) {
	if key := context.Get(r, "accessKey"); key != nil {
		helpers.WriteJSON(w, http.StatusOK, key.(db.AccessKey))
		return
	}

	project := context.Get(r, "project").(db.Project)
	var keys []db.AccessKey

	params := db.RetrieveQueryParams{
		SortBy: r.URL.Query().Get("sort"),
		SortInverted: r.URL.Query().Get("order") == desc,
	}

	keys, err := helpers.Store(r).GetAccessKeys(project.ID, params)

	if err != nil {
		helpers.WriteError(w, err)
		return
	}

	helpers.WriteJSON(w, http.StatusOK, keys)
}

// AddKey adds a new key to the database
func AddKey(w http.ResponseWriter, r *http.Request) {
	project := context.Get(r, "project").(db.Project)
	var key db.AccessKey

	if !helpers.Bind(w, r, &key) {
		return
	}

	if key.ProjectID == nil || *key.ProjectID != project.ID {
		helpers.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Project ID in body and URL must be the same",
		})
		return
	}

	switch key.Type {
	case "aws", "gcloud", "do":
		break
	case "ssh":
		if key.Secret == nil || len(*key.Secret) == 0 {
			helpers.WriteJSON(w, http.StatusBadRequest, map[string]string{
				"error": "SSH Secret empty",
			})
			return
		}
	default:
		helpers.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid key type",
		})
		return
	}

	*key.Secret += "\n"

	newKey, err := helpers.Store(r).CreateAccessKey(key)

	if err != nil {
		helpers.WriteError(w, err)
		return
	}

	objType := "key"

	desc := "Access Key " + key.Name + " created"
	_, err = helpers.Store(r).CreateEvent(db.Event{
		ProjectID:   newKey.ProjectID,
		ObjectType:  &objType,
		ObjectID:    &newKey.ID,
		Description: &desc,
	})

	if err != nil {
		log.Error(err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateKey updates key in database
// nolint: gocyclo
func UpdateKey(w http.ResponseWriter, r *http.Request) {
	var key db.AccessKey
	oldKey := context.Get(r, "accessKey").(db.AccessKey)

	if !helpers.Bind(w, r, &key) {
		return
	}

	switch key.Type {
	case "aws", "gcloud", "do":
		break
	case "ssh":
		if key.Secret == nil || len(*key.Secret) == 0 {
			helpers.WriteJSON(w, http.StatusBadRequest, map[string]string{
				"error": "SSH Secret empty",
			})
			return
		}
	default:
		helpers.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid key type",
		})
		return
	}

	if key.Secret == nil || len(*key.Secret) == 0 {
		// override secret
		key.Secret = oldKey.Secret
	} else {
		secret := *key.Secret + "\n"
		key.Secret = &secret
	}

	if err := helpers.Store(r).UpdateAccessKey(key); err != nil {
		helpers.WriteError(w, err)
		return
	}

	desc := "Access Key " + key.Name + " updated"
	objType := "key"

	_, err := helpers.Store(r).CreateEvent(db.Event{
		ProjectID:   oldKey.ProjectID,
		Description: &desc,
		ObjectID:    &oldKey.ID,
		ObjectType:  &objType,
	})

	if err != nil {
		log.Error(err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveKey deletes a key from the database
func RemoveKey(w http.ResponseWriter, r *http.Request) {
	key := context.Get(r, "accessKey").(db.AccessKey)

	var err error

	softDeletion := len(r.URL.Query().Get("setRemoved")) == 0

	if softDeletion {
		err = helpers.Store(r).DeleteAccessKeySoft(*key.ProjectID, key.ID)
	} else {
		err = helpers.Store(r).DeleteAccessKey(*key.ProjectID, key.ID)
		if err == db.ErrInvalidOperation {
			helpers.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": "Inventory is in use by one or more templates",
				"inUse": true,
			})
			return
		}
	}

	if err != nil {
		helpers.WriteError(w, err)
		return
	}

	desc := "Access Key " + key.Name + " deleted"

	_, err = helpers.Store(r).CreateEvent(db.Event{
		ProjectID:   key.ProjectID,
		Description: &desc,
	})

	if err != nil {
		log.Error(err)
	}

	w.WriteHeader(http.StatusNoContent)
}
