package sshghid

func Run(args []string) error {
	app, err := newApp()
	if err != nil {
		return err
	}
	return app.run(args)
}
