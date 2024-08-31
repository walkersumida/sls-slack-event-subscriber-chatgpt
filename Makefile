.PHONY: deploy undeploy

package:
	yarn sls package --verbose

deploy:
	yarn sls deploy --verbose

undeploy:
	yarn sls remove --verbose
