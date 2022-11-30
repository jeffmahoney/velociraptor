import Alert from 'react-bootstrap/Alert';
import humanizeDuration from "humanize-duration";

const English = {
    SEARCH_CLIENTS: "Search clients",
    "Quarantine description": <>
          <p>You are about to quarantine this host.</p>
          <p>
            While in quarantine, the host will not be able to
            communicate with any other networks, except for the
            Velociraptor server.
          </p>
                              </>,
    "Cannot Quarantine host": "Cannot Quarantine host",
    "Cannot Quarantine host message": (os_name, quarantine_artifact)=>
        <>
          <Alert variant="warning">
            { quarantine_artifact ?
              <p>This Velociraptor instance does not have the <b>{quarantine_artifact}</b> artifact required to quarantine hosts running {os_name}.</p> :
              <p>This Velociraptor instance does not have an artifact name defined to quarantine hosts running {os_name}.</p>
            }
          </Alert>
        </>,
    "Client ID": "Client ID",
    "Agent Version": "Agent Version",
    "Agent Name": "Agent Name",
    "First Seen At": "First Seen At",
    "Last Seen At": "Last Seen At",
    "Last Seen IP": "Last Seen IP",
    "Labels": "Labels",
    "Operating System": "Operating System",
    "Hostname": "Hostname",
    "FQDN": "FQDN",
    "Release": "Release",
    "Architecture": "Architecture",
    "Client Metadata": "Client Metadata",
    "Interrogate": "Interrogate",
    "VFS": "VFS",
    "Collected": "Collected",
    "Unquarantine Host": "Unquarantine Host",
    "Quarantine Host": "Quarantine Host",
    "Add Label": "Add Label",
    "Overview": "Overview",
    "VQL Drilldown": "VQL Drilldown",
    "Shell": "Shell",
    "Close": "Close",
    "Connected": "Connected",
    "time_ago": (value, unit) => {
        return value + ' ' + unit + ' ago';
    },
    "DeleteMessage": "You are about to permanently delete the following clients",
    "You are about to delete": name=>"You are about to delete "+name,
    "Edit Artifact": name=>{
        return "Edit Artifact " + name;
    },
    "Notebook for Collection": name=>"Notebook for Collection "+name,
    "ArtifactDeletionDialog": (session_id, artifacts, total_bytes, total_rows)=>
    <>
      You are about to permanently delete the artifact collection
      <b>{session_id}</b>.
      <br/>
      This collection had the artifacts <b className="wrapped-text">
                                          {artifacts}</b>
      <br/><br/>

      We expect to free up { total_bytes.toFixed(0) } Mb of bulk
      data and { total_rows } rows.
    </>,
    "ArtifactFavorites": artifacts=>
    <>
      You can easily collect the same collection from your
      favorites in future.
      <br/>
      This collection was the artifacts <b>{artifacts}</b>
      <br/><br/>
    </>,
    "DeleteHuntDialog": <>
                    <p>You are about to permanently stop and delete all data from this hunt.</p>
                    <p>Are you sure you want to cancel this hunt and delete the collected data?</p>
                        </>,
    "Notebook for Hunt": hunt_id=>"Notebook for Hunt " + hunt_id,
    "RecursiveVFSMessage": path=><>
       You are about to recursively fetch all files in <b>{path}</b>.
       <br/><br/>
        This may transfer a large amount of data from the endpoint. The default upload limit is 1gb but you can change it in the Collected Artifacts screen.
     </>,
    "ToolLocalDesc":
    <>
      Tool will be served from the Velociraptor server
      to clients if needed. The client will
      cache the tool on its own disk and compare the hash next
      time it is needed. Tools will only be downloaded if their
      hash has changed.
    </>,
    "ServedFromURL": (base_path, url)=>
    <>
      Clients will fetch the tool directly from
      <a href={base_path + url}>{url}</a> if
      needed. Note that if the hash does not match the
      expected hash the clients will reject the file.
    </>,
    "ServedFromGithub": (github_project, github_asset_regex)=>
    <>
      Tool URL will be refreshed from
      GitHub as the latest release from the project
      <b>{github_project}</b> that matches
      <b>{github_asset_regex}</b>
    </>,
    "PlaceHolder":
    <>
      Tool hash is currently unknown. The first time the tool
      is needed, Velociraptor will download it from it's
      upstream URL and calculate its hash.
    </>,
    "ToolHash":
    <>
      Tool hash has been calculated. When clients need to use
      this tool they will ensure this hash matches what they
      download.
    </>,
    "AdminOverride":
    <>
      Tool was manually uploaded by an
      admin - it will not be automatically upgraded on the
      next Velociraptor server update.
    </>,
    "ToolError":
    <>
      Tool's hash is not known and no URL
      is defined. It will be impossible to use this tool in an
      artifact because Velociraptor is unable to resolve it. You
      can manually upload a file.
    </>,
    "OverrideToolDesc":
    <>
      As an admin you can manually upload a
      binary to be used as that tool. This will override the
      upstream URL setting and provide your tool to all
      artifacts that need it. Alternative, set a URL for clients
      to fetch tools from.
    </>,
    "X per second": x=><>{x} per second</>,
    "HumanizeDuration": difference=>{
        if (difference<0) {
            return <>
                     In {humanizeDuration(difference, {
                         round: true,
                         language: "en",
                     })}
                   </>;
        }
        return <>
                 {humanizeDuration(difference, {
                     round: true,
                     language: "en",
                 })} ago
               </>;
    },
    "EventMonitoringCard":
    <>
      Event monitoring targets specific label groups.
      Select a label group above to configure specific
      event artifacts targetting that group.
    </>,
    "Deutsch": "German",
    "_ts": "Server Time",
    "TablePagination": (from, to, size)=>
    <>Showing { from } to { to } of { size }</>,
    "Verified Email" : "Verified Email",
    "Account Locked" : "Account Locked",
    "Role_administrator" : "Server Administrator",
    "Role_org_admin" : "Organization Administrator",
    "Role_reader" : "Read-Only User",
    "Role_analyst" : "Analyst",
    "Role_investigator" : "Investigator",
    "Role_artifact_writer" : "Artifact Writer",
    "Role_api" : "Read-Only API Client",
    "ToolRole_administrator" :
    <>
    Like any system, Velociraptor needs an administrator which is all powerful. This account can run arbitrary VQL on the server, reconfigure the server, etc.  The ability to add/create/edit/remove users is dependent on the organizations to which this account belongs.
    </>,
    "ToolRole_org_admin" :
    <>
    This role provides the ability to manage organizations.  It would typically be used together with another role.
    </>,
    "ToolRole_reader" :
    <>
    This role provides the ability to read previously collected results but does not allow the user to actually make any changes.  This role is useful to give unpriviledged users visibility into what information is being collected without giving them access to modify anything.
    </>,
    "ToolRole_analyst" :
    <>
    This role provides the ability to read existing collected data and also run some server side VQL in order to do post processing of this data or annotate it. Analysts typically use the notebook or download collected data offline for post processing existing hunt data. Analysts may not actually start new collections or hunts themselves.
    </>,
    "ToolRole_investigator" :
    <>
    This role provides the ability to read existing collected data and also run some server side VQL in order to do post processing of this data or annotate it. Investigators typically use the notebook or download collected data offline for post processing existing hunt data. Investigators may start new collections or hunts themselves.
    </>,
    "ToolRole_artifact_writer" :
    <>
    This role allows a user to create or modify new client side artifacts (They are not able to modify server side artifacts). This user typically has sufficient understanding and training in VQL to write flexible artifacts. Artifact writers are very powerful as they can easily write a malicious artifact and collect it on the endpoint. Therefore they are equivalent to domain admins on endpoints. You should restrict this role to very few people.
    </>,
    "ToolRole_api" :
    <>
    This role provides the ability to read previously collected results but does not allow the user to actually make any changes.
    </>,
    "ToolPerm_all_query" : "Issue all queries without restriction",
    "ToolPerm_any_query" : "Issue any query at all (AllQuery implies AnyQuery)",
    "ToolPerm_pubish" : "Publish events to server side queues (typically not needed)",
    "ToolPerm_read_results" : "Read results from already run hunts, flows, or notebooks",
    "ToolPerm_label_clients" : "Can manipulate client labels and metadata",
    "ToolPerm_collect_client" : "Schedule or cancel new collections on clients",
    "ToolPerm_collect_server" : "Schedule new artifact collections on Velociraptor servers",
    "ToolPerm_artifact_writer" : "Add or edit custom artifacts that run on the server",
    "ToolPerm_server_artifact_writer" : "Add or edit custom artifacts that run on the server",
    "ToolPerm_execve" : "Allowed to execute arbitrary commands on clients",
    "ToolPerm_notebook_editor" : "Allowed to change notebooks and cells",
    "ToolPerm_server_admin" : "Allowed to manage server configuration",
    "ToolPerm_org_admin" : "Allowed to manage organizations",
    "ToolPerm_impersonation" : "Allows the user to specify a different username for the query() plugin",
    "ToolPerm_filesystem_read" : "Allowed to read arbitrary files from the filesystem",
    "ToolPerm_filesystem_write" : "Allowed to create files on the filesystem",
    "ToolPerm_machine_state" : "Allowed to collect state information from machines (e.g. pslist())",
    "ToolPerm_prepare_results" : "Allowed to create zip files",
    "ToolPerm_datastore_access" : " Allowed raw datastore access",
    "ToolUser_verified_email" : "The email address for this user has been verified",
    "ToolUser_locked" : "This account is locked.",
    "ToolUsernamePasswordless" :
    <>
        This server is configured to authenticate users using an external authentication host.  This account must exist on the authentication system for login to be successful.
    </>,
    "Add User" : "Add User",
    "Update User" : "Update User",
    "Roles" : "Roles",
    "Roles in": org => {
    return "Roles in " + org;
    },
    "Not a member of": org => {
        return "Not a member of " + org;
    },

    "ToolRoleBasedPermissions" :
    <>
    Role-Based Permissions allow the administrator to grant sets of permissions for common activities.  A user may have multiple roles assigned.
    </>,
    "ToolEffectivePermissions" :
    <>
    Roles are defined as sets of fine-grained permissions.  This is the current set of permissions defined by the roles for this user.
    </>,
    "ToolOrganizations" :
    <>
    Organizations allow multiple tenants to use this Velociraptor server.  If a user is not assigned to an organization, it is a member of the Organizational Root, which implies membership in all organizations.
    </>,
    "User does not exist": (username)=><>User {username} does not exist.</>,
    "Do you want to delete?": (username)=>"Do you want to delete " + username + "?",
    "WARN_REMOVE_USER_FROM_ORG": (user, org)=>(
        <>
          You are about to remove user {user} from Org {org} <br/>
        </>),
};

export default English;
