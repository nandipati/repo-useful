#!/bin/bash

# set git repo name
GIT_REPO_NAME=io.hmheng.riverside-salt


# clear screen
clear

# script requires jq - confirm jq install
if ! [[ $(jq -V | grep 'jq') ]]; then
  echo "ERROR: script requires jq - please install jq and try again";
  exit 1;
fi

# get code deploy applications list via aws cli
CD_APP_LIST_JSON=`aws deploy list-applications --query applications --output json`

# count items in code deploy applications list with jq
CD_APP_COUNT=`echo ${CD_APP_LIST_JSON} | jq '. | length'`

# add display title - application(s)
echo "CODE DEPLOY APPLICATIONS"
echo "-----------------------------------------------------";

# set base count
COUNT=0

# loop through available code deploy applications
while [ ${COUNT} -lt ${CD_APP_COUNT} ]; do
 CD_APP_LIST_IND=`echo ${CD_APP_LIST_JSON} | jq -r ".[${COUNT}]"`;
 echo "${COUNT}     ${CD_APP_LIST_IND}";
 let COUNT=COUNT+1
done

# request user input - application(s)
echo "-----------------------------------------------------";
echo "please enter application number to deploy: "

# get user input -> application
read CD_APP_SELECT_ID

# set code deploy application name based on user input
CD_APP_SELECT_NAME=`echo ${CD_APP_LIST_JSON} | jq -r ".[${CD_APP_SELECT_ID}]"`

# clear screen
clear

# get deployment groups for selected application via aws cli
CD_DEPGROUP_LIST_JSON=`aws deploy list-deployment-groups --application-name ${CD_APP_SELECT_NAME} --query deploymentGroups[] --output json`

# count items in code deploy deployment groups list with jq
CD_DEPGROUP_COUNT=`echo ${CD_DEPGROUP_LIST_JSON} | jq '. | length'`

# add display title - deployment group(s)
echo "CODE DEPLOY DEPLOYMENT GROUPS"
echo "-----------------------------------------------------";

# set base count
COUNT=0

# loop through available code deploy deployment groups
while [ ${COUNT} -lt ${CD_DEPGROUP_COUNT} ]; do
 CD_DEPGROUP_LIST_IND=`echo ${CD_DEPGROUP_LIST_JSON} | jq -r ".[${COUNT}]"`;
 echo "${COUNT}     ${CD_DEPGROUP_LIST_IND}";
 let COUNT=COUNT+1
done

# request user input - deployment group(s)
echo "-----------------------------------------------------";
echo "please enter deployment group number to deploy to: "

# get user input -> deployment group
read CD_DEPGROUP_SELECT_ID

# set code deploy deployment group name based on user input
CD_DEPGROUP_SELECT_NAME=`echo ${CD_DEPGROUP_LIST_JSON} | jq -r ".[${CD_DEPGROUP_SELECT_ID}]"`

# clear screen
clear

# get git origin and remote urls
GIT_ORIGIN_URL=`git config --get remote.origin.url`
GIT_UPSTREAM_URL=`git config --get remote.upstream.url`

# cut username from git urls
function parse_git_user_https {
  cut -d'/' -f4 $@
}
function parse_git_user_ssh {
  cut -d':' -f2 | cut -d'/' -f1 $@
}
if [[ $GIT_ORIGIN_URL == *"https"* ]]
then
  GIT_ORIGIN_USERNAME=`echo ${GIT_ORIGIN_URL} | parse_git_user_https`
  GIT_UPSTREAM_USERNAME=`echo ${GIT_UPSTREAM_URL} | parse_git_user_https`
else
  GIT_ORIGIN_USERNAME=`echo ${GIT_ORIGIN_URL} | parse_git_user_ssh`
  GIT_UPSTREAM_USERNAME=`echo ${GIT_UPSTREAM_URL} | parse_git_user_ssh`
fi

# add display info - git user
echo "GIT USER ACCOUNTS TO DEPLOY FROM"
echo "-----------------------------------------------------";
echo "0     ${GIT_ORIGIN_USERNAME}"
echo "1     ${GIT_UPSTREAM_USERNAME}"
echo "-----------------------------------------------------";
echo "please enter git account to deploy from: "

# get user input -> git user account
read GIT_USERNAME_SELECT_ID

# set git user account based on user input
if [ ${GIT_USERNAME_SELECT_ID} == 0 ]; then
 GIT_USERNAME_SELECT_NAME=${GIT_ORIGIN_USERNAME};
 GIT_USERNAME_SELECT_URL=${GIT_ORIGIN_URL};
elif [ ${GIT_USERNAME_SELECT_ID} == 1 ]; then
 GIT_USERNAME_SELECT_NAME=${GIT_UPSTREAM_USERNAME};
 GIT_USERNAME_SELECT_URL=${GIT_UPSTREAM_URL};
else
 echo "ERROR: invalid selection"
 exit 1
fi

# clear screen
clear

# get git current commit id, which will be deployed
GIT_COMMIT_ID=`git log --pretty=format:'%H' -n 1`

# confirmation
echo "CONFIRMATION"
echo "-----------------------------------------------------";
echo "APPLICATION: ${CD_APP_SELECT_NAME}"
echo "DEPLOYMENT GROUP: ${CD_DEPGROUP_SELECT_NAME}"
echo "GIT REPO: ${GIT_USERNAME_SELECT_URL}"
echo "GIT COMMIT: ${GIT_COMMIT_ID}"
echo "-----------------------------------------------------";
echo "START DEPLOYMENT? (y/n) : "

# get confirmation input
read CONFIRMATION_INPUT

# process confirmation input (deploy or cancel)
if [ ${CONFIRMATION_INPUT} == 'y' ]; then
 echo "INFO: starting deployment process";
 aws deploy create-deployment \
  --application-name ${CD_APP_SELECT_NAME} \
  --deployment-config-name CodeDeployDefault.AllAtOnce \
  --deployment-group-name ${CD_DEPGROUP_SELECT_NAME} \
  --github-location repository=${GIT_USERNAME_SELECT_NAME}/${GIT_REPO_NAME},commitId=${GIT_COMMIT_ID}
 echo "INFO: deployment process complete";
elif [ ${CONFIRMATION_INPUT} == 'n' ]; then
 echo 'INFO: deployment cancelled';
 exit 99;
else
 echo 'ERROR: invalid selection';
 exit 1
fi
