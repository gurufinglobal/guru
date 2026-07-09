package app

func (app *App) mountStoresAndSetABCIHandlers() {
	app.MountKVStores(app.GetKVStoreKeys())
	app.MountTransientStores(app.GetTransientStoreKeys())
	app.MountObjectStores(app.GetObjectStoreKeys())

	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
}
