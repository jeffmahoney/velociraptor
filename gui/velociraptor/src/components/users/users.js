import React from 'react';
import api from '../core/api-service.js';
import axios from 'axios';
import VeloTable from '../core/table.js';
import Button from 'react-bootstrap/Button';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import User from './user.js';
import './users.css';

export default class Users extends React.Component {

    componentDidMount = () => {
        this.source = axios.CancelToken.source();
        this.loadUsers();
    }

    componentWillUnmount() {
        this.source.cancel("unmounted");
    }

    componentDidUpdate = () => {
        if (!this.state.initialized) {
            this.loadUsers();
        }
    }

    state = {
        users: [],
        columns: ["Username", "Permissions", "Roles", "Functions"],
        initialized: false
    }

    handleDelete = (name, e) => {
        e.preventDefault();
        var result = window.confirm("Do you want to delete " + name + "?");
        if (result) {
            api.delete_req("v1/DeleteUser/" + name, {}, this.source.token);
            this.loadUsers();
        }
    }

    loadUsers = () => {
        api.get("v1/GetUsers", {}, this.source.token).then((response) => {
            let names = [];
            for(var i = 0; i<response.data.users.length; i++) {
                var name = response.data.users[i].name;
                delete response.data.users[i].name;
                var roles = "";
                var permissions = {};
                if (response.data.users[i].Permissions) {
                    permissions = response.data.users[i].Permissions;
                    if (permissions.roles) {
                        roles += response.data.users[i].Permissions.roles;
                        delete permissions.roles;
                    }
                    delete response.data.users[i].Permissions;
                }
                names.push({Username: name, Permissions: permissions, Roles: roles, Functions: name});
            }
            this.setState({users: names, initialized: true});
        });
    };

    render() {
        let renderers = {
            Functions: (cell) => {
                return <>
                         <Button id="delete_user"
                                 onClick={(e) => this.handleDelete(cell, e)}
                                 variant="danger">
                           <FontAwesomeIcon icon="window-close"/>
                         </Button>
                         <Button id="change_user"
                                 href={window.location.href + "/" + cell}
                                 variant="default">
                           <FontAwesomeIcon icon="pencil-alt"/>
                         </Button>
                       </>;
            },
        };

        return(
              <div className="users">
                <div className="mb-4">
                <User loadUsers={this.loadUsers}/>
                </div>
                <h2>Users</h2>
                <VeloTable rows={this.state.users}
                           columns={this.state.columns}
                           renderers={renderers} />
              </div>
        );
    }
};
