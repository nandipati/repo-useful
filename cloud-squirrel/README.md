Cloud Squirrel
========================

![Cloud Squirrel](images/cs.jpg)

The Cloud Squirrel application(cs) is a command line application for the Riverside Cloud Services(RCS).
The application provides custom additional functionalities like for example support for quotas.

The application is implemented as a wrapper on top of nomad, consul and docker.

The application supports the following commands:
* cs quota init <quota_key> <limit>
* cs quota usage
* cs run <job_file.nomad>
* cs nomad <....any nomad command (except run)>
* cs builder ...




Install
==========
```
git clone https://github.com/rsinsights/rcs-tools.git
cd cloud-squirrel
make install
```

The application gets config params from properties file.

Edit properties file
==========
The properties file is located in the user's home directory:
```
 $HOME/.cs/cs.properties
```

Start event listener for consul events
==========
The app uses "consul watches" to listen for consul events and update the quota usage in consul when there is a change in
the nomad cluster. The quota usage auto update functionality is deployed as a docker container running in nomad(consul-events-monitor).



Usage
==========
Get help:
```
cs --help
```

Run a nomad job file:
```
cs run nomad_jobfile.nomad
```


Check if a job is running:
```
 cs nomad status
```


Stop a job:
```
 cs nomad stop <jobID>
```



### Run a nomad job:

[Deploy applications on RCS](https://github.com/rsinsights/rcs-tools/wiki/Deploy-applications-on-RCS)


### Run a nomad job by specifying a Docker artifact ID:
```
cs run-artifact-id  "rcs/SHDJSDGJSHGDS239829382-clean-sweep:1.01"   job_file.nomad
```

### Init and show quota:

[Init quota for RCS](https://github.com/rsinsights/rcs-tools/wiki/Init-quota-for-RCS)



The quota limits and the usages are available in the Consul GUI in the Key/Value section. Example:
```
/quotas/limit/rcscorenp--rcs_infra--cpu 4000
/quotas/limit/rcscorenp--rcs_infra--memory 32000
/quotas/usage/rcscorenp--rcs_infra--cpu 1000
/quotas/usage/rcscorenp--rcs_infra--memory 2000


```


