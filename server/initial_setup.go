package server

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/ffmpeg"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/utils/pwdstrength"
)

func initialSetup(ds model.DataStore) {
	ctx := context.TODO()
	_ = ds.WithTx(func(tx model.DataStore) error {
		if err := tx.Library(ctx).StoreMusicFolder(); err != nil {
			return err
		}

		properties := tx.Property(ctx)
		_, err := properties.Get(consts.InitialSetupFlagKey)
		if err == nil {
			return nil
		}
		log.Info("Running initial setup")
		if conf.Server.DevAutoCreateAdminPassword != "" {
			if err = createInitialAdminUser(tx, conf.Server.DevAutoCreateAdminPassword); err != nil {
				return err
			}
		}

		err = properties.Put(consts.InitialSetupFlagKey, time.Now().String())
		return err
	}, "initial setup")
}

// If the Dev Admin user is not present, create it
func createInitialAdminUser(ds model.DataStore, initialPassword string) error {
	users := ds.User(context.TODO())
	c, err := users.CountAll(model.QueryOptions{Filters: squirrel.Eq{"user_name": consts.DevInitialUserName}})
	if err != nil {
		panic(fmt.Sprintf("Could not access User table: %s", err))
	}
	if c == 0 {
		// Guarded even though this is a dev convenience: it is set through
		// configuration, and configuration set in a container env is exactly how a
		// weak admin password reaches production.
		if res := pwdstrength.Evaluate(initialPassword, consts.DevInitialUserName, ""); res.Level != pwdstrength.Strong {
			return fmt.Errorf("DevAutoCreateAdminPassword is %s and must be strong: %s",
				res.Level, pwdstrength.Describe(res.Reasons))
		}
		newID := id.NewRandom()
		log.Warn("Creating initial admin user. This should only be used for development purposes!!",
			"user", consts.DevInitialUserName, "password", initialPassword, "id", newID)
		initialUser := model.User{
			ID:          newID,
			UserName:    consts.DevInitialUserName,
			Name:        consts.DevInitialName,
			Email:       "",
			NewPassword: initialPassword,
			IsAdmin:     true,
		}
		// Shadowing this would swallow the error and leave setup silently
		// admin-less — including when DevAutoCreateAdminPassword is not strong
		// enough to pass the password bar.
		err = users.Put(&initialUser)
		if err != nil {
			log.Error("Could not create initial admin user. Is DevAutoCreateAdminPassword strong enough?",
				"user", consts.DevInitialUserName, err)
			return err
		}
	}
	return err
}

func checkFFmpegInstallation() {
	f := ffmpeg.New()
	_, err := f.CmdPath()
	if err != nil {
		log.Warn("Unable to find ffmpeg. Transcoding will fail if used", err)
		if conf.Server.Scanner.Extractor == "ffmpeg" {
			log.Warn("ffmpeg cannot be used for metadata extraction. Falling back to taglib")
			conf.Server.Scanner.Extractor = "taglib"
		}
		return
	}
	if !f.IsProbeAvailable() {
		log.Warn("Unable to find ffprobe. Transcoding decisions will be limited")
	}
}

func checkExternalCredentials() {
	if conf.Server.EnableExternalServices {
		if !conf.Server.LastFM.Enabled {
			log.Info("Last.fm integration is DISABLED")
		} else {
			log.Debug("Last.fm integration is ENABLED")
		}

		if !conf.Server.ListenBrainz.Enabled {
			log.Info("ListenBrainz integration is DISABLED")
		} else {
			log.Debug("ListenBrainz integration is ENABLED", "ListenBrainz.BaseURL", conf.Server.ListenBrainz.BaseURL)
		}
	}
}
