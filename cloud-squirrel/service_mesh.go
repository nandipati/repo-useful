package main

func updateMeshemEnvoyProxy(serviceName string) {

	loop :
	wait until the serice shows up in consul
	get IP and PORT for each service instance and update meshem using meshem ctl
	
	substancesAddresses [] hostAddress =	getAllSubstanceAddressesFromConsul(serviceName)
	
	// looop and build the meshem command for each substance address
	
	meshemCmd : = "---
protocol: HTTP
hosts:
  - name: reporting-api-dev-meshem-01
    ingressAddr:
      host: 10.2.1.11
      port: 80
    substanceAddr:
      host: 10.34.226.73
      port: 26702
    egressHost: 10.2.1.11
  - name: reporting-api-dev-meshem-02
    ingressAddr:
      host: 10.2.1.12
      port: 80
    substanceAddr:
      host: 10.34.226.73
      port: 23941
    egressHost: 10.2.1.12
dependentServices: []"


    meshemCommandFileName := "meshem_command.yml"

    saveToFile(meshemCommandFileName, meshemCmd)
	
	exec_shell_cmd("meshemctl svc apply %s -f %s ",serviceName,  meshemCommandFileName)
}


func getAllSubstanceAddressesFromConsul(){
}





// #!/bin/bash

// set -eux



// export MESHEM_CTLAPI_ENDPOINT=http://localhost:18091

// meshemctl svc apply reporting-api-dev -f ../app1/reporting-api-dev-svc.yml
// meshemctl svc apply front -f reporting-svc.yml
// curl 'http://consul:8500/v1/kv/hosts?token=master&keys=true'
// curl 'http://consul:8500/v1/catalog/nodes?token=master'






// [ec2-user@ip-10-34-226-249 front]$ cat reporting-svc.yml
// ---
// protocol: HTTP
// hosts:
//   - name: front
//     ingressAddr:
//       host: 10.2.2.11
//       port: 8088
//     substanceAddr:
//       host: 10.2.2.1
//       port: 8080
//     egressHost: 10.2.2.11
// dependentServices:
//   - name: reporting-api-dev
// egressPort: 9000




// [ec2-user@ip-10-34-226-249 front]$ cat ../app1/reporting-api-dev-svc.yml


// ---
// protocol: HTTP
// hosts:
//   - name: reporting-api-dev-meshem-01
//     ingressAddr:
//       host: 10.2.1.11
//       port: 80
//     substanceAddr:
//       host: 10.34.226.73
//       port: 26702
//     egressHost: 10.2.1.11
//   - name: reporting-api-dev-meshem-02
//     ingressAddr:
//       host: 10.2.1.12
//       port: 80
//     substanceAddr:
//       host: 10.34.226.73
//       port: 23941
//     egressHost: 10.2.1.12
// dependentServices: []






// docker-compose.yml
// version: '2'

// services:

//   meshem:
//     build:
//       context: .
//       dockerfile: Dockerfile-meshem
//     volumes:
//       - ../../.:/go/src/github.com/rerorero/meshem
//     expose:
//       - 8090
//       - 8091
//     ports:
//       - 18091:8091
//     networks:
//       test:
//         ipv4_address: 10.2.0.2

//   app1-01:
//     build:
//       context: ./app1
//       dockerfile: Dockerfile
//     environment:
//       message: "respond from app1-01"
//     expose: [9001]
//     networks:
//       test:
//         ipv4_address: 10.2.1.1
//   envoy-app1-01:
//     build:
//       context: .
//       dockerfile: Dockerfile-envoy
//     volumes:
//       - ./envoy.yaml:/etc/envoy.yaml
//     #command: "/usr/local/bin/envoy  -c /etc/envoy.yaml -l trace --service-cluster app1 --service-node app1-01"
//     command: "/usr/local/bin/envoy  -c /etc/envoy.yaml -l trace --service-cluster reporting-api-dev-meshem --service-node reporting-api-dev-meshem-01"
//     expose:
//       - "80"
//       - "9000"
//       - "8001"
//     ports:
//       - 18001:8001
//       - 18088:8088
//       - 19000:9000
//     networks:
//       test:
//         ipv4_address: 10.2.2.11

//   zipkin:
//     image: openzipkin/zipkin
//     expose:
//       - "9411"
//     ports:
//       - "19411:9411"
//     networks:
//       test:
//         ipv4_address: 10.2.3.1

//   client:
//     build:
//       context: .
//       dockerfile: Dockerfile-client
//     volumes:
//       - ../../.:/go/src/github.com/rerorero/meshem
//     networks:
//       test:
//         ipv4_address: 10.2.0.3

// networks:
//   test:
//     driver: bridge
//     ipam:
//       config:
//         - subnet: 10.2.0.0/16
//           gateway: 10.2.0.1
