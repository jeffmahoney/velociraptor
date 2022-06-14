import React from 'react';
import { withRouter } from 'react-router-dom';
import api from '../core/api-service.js';
import axios from 'axios';
import FormControl from 'react-bootstrap/FormControl';
import Button from 'react-bootstrap/Button';
import InputGroup from 'react-bootstrap/InputGroup';
import FormGroup from 'react-bootstrap/FormGroup';
import Form from 'react-bootstrap/Form';
import Dropdown from 'react-bootstrap/Dropdown';
import ToggleButton from 'react-bootstrap/ToggleButton';
import ToggleButtonGroup from 'react-bootstrap/ToggleButtonGroup';

export class User extends React.Component {

    componentDidMount = () => {
        this.source = axios.CancelToken.source();
        if (this.props.match && this.props.match.params &&
            this.props.match.params.user) {
            this.loadUser();
        } else {
            this.setState({new_user: true});
        }
    }

    componentWillUnmount() {
        this.source.cancel("unmounted");
    }

    state = {
        name: "",
        password: "",
        email: "",
        picture: "",
        verified_email: false,
        read_only: false,
        locked: false,
        permissions: {
            all_query: false,
            read_results: false,
            collect_client: false,
            collect_server: false,
            artifact_writer: false,
            execve: false,
            notebook_editor: false,
            any_query: false,
            label_clients: false,
            server_admin: false,
            filesystem_read: false,
            filesystem_write: false,
            server_artifact_writer: false,
            machine_state: false,
            prepare_results: false,
            datastore_access: false,
            roles: [],
        },
        selected_roles: {
            administrator: false,
            reader: false,
            analyst: false,
            investigator: false,
            artifact_writer: false,
            api: false,
        },
        new_user: false
    }

    handleCreate = (e) => {
        e.preventDefault();

        api.post("v1/CreateUser", {name: this.state.name,
                                   password: this.state.password,
                                   email: this.state.email,
                                   verified_email: this.state.verified_email,
                                   read_only: this.state.read_only,
                                   locked: this.state.locked,
                                   Permissions: this.state.permissions}, this.source.token);

        if (this.props.loadUsers) {
            this.props.loadUsers();
        }
        if (!this.state.new_user) {
            this.props.history.push("/users");
        }
    }

    loadUser = () => {
        let user = this.props.match && this.props.match.params &&
            this.props.match.params.user;
        api.get("v1/GetUser/" + user, {}, this.source.token).then((response) => {
            for (const [data_key, data_value] of Object.entries(response.data)) {
                if (data_key === "Permissions") {
                    for (const [perm_key, perm_value] of Object.entries(data_value)) {
                        if (perm_key === "roles") {
                            perm_value.forEach(e => this.setState(prevState => {
                                let selected_roles = Object.assign({}, prevState.selected_roles);
                                selected_roles[e] = true;
                                return {selected_roles};
                            }));
                            this.setState(prevState => {
                                let permissions = Object.assign({}, prevState.permissions);
                                permissions[perm_key] = perm_value;
                                return {permissions};
                            });
                        } else {
                            this.setState(prevState => {
                                let permissions = Object.assign({}, prevState.permissions);
                                permissions[perm_key] = perm_value;
                                return {permissions};
                            });
                        }
                    }
                } else {
                    this.setState(prevState => {
                        prevState[data_key] = data_value;
                        return prevState;
                    });
                }
            }
        });
    };

    handleRoleSelect = (role) => {
        this.setState(prevState => {
            let selected_roles = Object.assign({}, prevState.selected_roles);
            let permissions = Object.assign({}, prevState.permissions);
            selected_roles[role] = !selected_roles[role];
            if (selected_roles[role]) {
                permissions.roles.push(role);
            } else {
                permissions.roles = permissions.roles.filter(p_role => p_role !== role);
            }
            return {selected_roles, permissions};
        });
    }

    handlePermissionSelect = (permission) => {
        this.setState(prevState => {
            let permissions = Object.assign({}, prevState.permissions);
            permissions[permission] = !permissions[permission];
            return {permissions};
        });
    }

    render() {
        let permission_items = Object.assign({}, this.state.permissions);
        delete permission_items.roles;

        let nameForm;
        if (this.state.new_user) {
            nameForm = <FormGroup>
                         <FormControl placeholder="Username"
                                      aria-label="Username"
                                      aria-describedby="basic-addon1"
                                      onChange={(e) => this.setState({name: e.target.value})}/>
                       </FormGroup>;
        }

        return(
                <>
                  <h2 className="mx-1">{this.state.new_user ? "Create a new user" : "Edit user: " + this.state.name}</h2>
                  <div className="pt-1 col-12">
                    <Form>
                      { nameForm }
                      <FormGroup>
                        <FormControl placeholder="Password"
                                     aria-label="Password"
                                     type="password"
                                     aria-describedby="basic-addon1"
                                     onChange={(e) => this.setState({password: e.target.value})}
                          />
                      </FormGroup>
                      <FormGroup>
                        <FormControl placeholder="Email"
                                     aria-label="Email"
                                     aria-describedby="basic-addon1"
                                     onChange={(e) => this.setState({email: e.target.value})}
                                     value={this.state.email}/>
                      </FormGroup>
                      <FormGroup>
                        <InputGroup className="mb-3">
                          <DropdownList onItemToggle={(c)=>{this.handlePermissionSelect(c);}}
                                        name="Permissions"
                                        items={permission_items}/>
                          <DropdownList onItemToggle={(c)=>{this.handleRoleSelect(c);}}
                                        name="Roles"
                                        items={this.state.selected_roles}/>
                        </InputGroup>
                        <InputGroup>
                          <ToggleButtonGroup type="checkbox"
                                             value={[]}>
                            <ToggleButton onChange={() => {this.setState({verified_email: !this.state.verified_email});}}
                                          variant={this.state.verified_email ? "default" : "primary"}
                                          id="verified-email-button"
                                          value="verified-email"
                                          type="checkbox">
                              verified-email
                            </ToggleButton>
                          </ToggleButtonGroup>
                          <ToggleButtonGroup type="checkbox"
                                             value={[]}>
                            <ToggleButton onChange={() => {this.setState({read_only: !this.state.read_only});}}
                                          variant={this.state.read_only ? "default" : "primary"}
                                          id="read-only-button"
                                          value="read-only"
                                          type="checkbox">
                              read-only
                            </ToggleButton>
                          </ToggleButtonGroup>
                          <ToggleButtonGroup type="checkbox"
                                             value={[]}>
                            <ToggleButton onChange={() => {this.setState({locked: !this.state.locked});}}
                                          variant={this.state.locked ? "default" : "primary"}
                                          id="locked-button"
                                          value="locked"
                                          type="checkbox">
                              locked
                            </ToggleButton>
                          </ToggleButtonGroup>
                        </InputGroup>
                      </FormGroup>
                      <Button onClick={this.handleCreate}
                              variant="default" type="submit">
                        {this.state.new_user ? "Add user" : "Apply changes"}
                      </Button>
                    </Form>
                  </div>
                </>
        );
    }
};

export const DropdownList = (e) => {
    const [open, setOpen] = React.useState(false);
    const onToggle = (isOpen, _, metadata) => {
        if (metadata.source === "select") {
            setOpen(true);
            return;
        }
        setOpen(isOpen);
    };

    const { items, onItemToggle, name } = e;
    let enabled_items = [];

    let buttons = Object.keys(items).map((item) => {
        if (items[item]) {
            enabled_items.push(item);
        }
        return <Dropdown.Item key={item}
                              eventKey={item}
                              active={items[item]}
                              onSelect={c=>onItemToggle(c)}>
                 { item }
               </Dropdown.Item>;
    });

    return (
        <Dropdown show={open} onToggle={onToggle}>
          <Dropdown.Toggle variant="default" id="dropdown-basic">
              { name }
          </Dropdown.Toggle>

          <Dropdown.Menu>
            { buttons }
          </Dropdown.Menu>
        </Dropdown>
    );
};

export default withRouter(User);
