.PHONY: deploy undeploy

deploy:
	yarn sls deploy --verbose

undeploy:
	yarn sls remove --verbose
