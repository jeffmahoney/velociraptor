package api

import (
	context "golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"www.velocidex.com/golang/velociraptor/acls"
	api_proto "www.velocidex.com/golang/velociraptor/api/proto"
	users "www.velocidex.com/golang/velociraptor/users"
)

func (self *ApiServer) GetUsers(
	ctx context.Context,
	in *emptypb.Empty) (*api_proto.Users, error) {

	user_name := GetGRPCUserInfo(self.config, ctx, self.ca_pool).Name
	user_record, err := users.GetUser(self.config, user_name)
	if err != nil {
		return nil, err
	}

	permissions := acls.READ_RESULTS
	perm, err := acls.CheckAccess(self.config, user_record.Name, permissions)
	if !perm || err != nil {
		return nil, status.Error(codes.PermissionDenied,
			"User is not allowed to enumerate users.")
	}

	result := &api_proto.Users{}

	users, err := users.ListUsers(self.config)
	if err != nil {
		return nil, err
	}

	result.Users = users

	return result, nil
}

func (self *ApiServer) DeleteUser(
	ctx context.Context,
	in *api_proto.UserRequest) (*emptypb.Empty, error) {

	user_name := GetGRPCUserInfo(self.config, ctx, self.ca_pool).Name
	user_record, err := users.GetUser(self.config, user_name)
	if err != nil {
		return nil, err
	}

	permissions := acls.SERVER_ADMIN
	perm, err := acls.CheckAccess(self.config, user_record.Name, permissions)
	if !perm || err != nil {
		return nil, status.Error(codes.PermissionDenied,
			"User is not allowed to delete users.")
	}

	err = users.DeleteUser(self.config, in.Name)
	if err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (self *ApiServer) CreateUser(
	ctx context.Context, in *api_proto.CreateUserRequest) (*api_proto.VelociraptorUser, error) {

	user_name := GetGRPCUserInfo(self.config, ctx, self.ca_pool).Name
	user_record, err := users.GetUser(self.config, user_name)
	if err != nil {
		return nil, err
	}

	permissions := acls.SERVER_ADMIN
	perm, err := acls.CheckAccess(self.config, user_record.Name, permissions)
	if !perm || err != nil {
		return nil, status.Error(codes.PermissionDenied,
			"User is not allowed to create users.")
	}

	// require user role
	if len(in.Permissions.Roles) == 0 {
		return nil, status.Error(codes.Aborted, "Role needs to specified.")
	}

	user, err := users.GetUserWithPermissions(self.config, in.Name)
	if err != nil {
		user, err = users.NewUserRecord(in.Name)
		if err != nil {
			return nil, err
		}
	}

	if in.Password != "" {
		users.SetPassword(user, in.Password)
	}

	err = users.SetUserPermissions(self.config, in.Name, in.Permissions)
	if err != nil {
		return nil, err
	}

	user.Email = in.Email
	user.VerifiedEmail = in.VerifiedEmail
	user.ReadOnly = in.ReadOnly
	user.Locked = in.Locked

	err = users.SetUser(self.config, user)
	if err != nil {
		return nil, err
	}

	result, err := users.GetUser(self.config, in.Name)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Get single user
func (self *ApiServer) GetUser(
	ctx context.Context, in *api_proto.UserRequest) (*api_proto.VelociraptorUser, error) {

	user_name := GetGRPCUserInfo(self.config, ctx, self.ca_pool).Name
	user_record, err := users.GetUser(self.config, user_name)
	if err != nil {
		return nil, err
	}

	permissions := acls.SERVER_ADMIN
	perm, err := acls.CheckAccess(self.config, user_record.Name, permissions)
	if !perm || err != nil {
		return nil, status.Error(codes.PermissionDenied,
			"User is not allowed to view this user.")
	}

	user, err := users.GetUserWithPermissions(self.config, in.Name)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (self *ApiServer) GetUserFavorites(
	ctx context.Context,
	in *api_proto.Favorite) (*api_proto.Favorites, error) {

	// No special permission requires to view a user's own favorites.
	user_name := GetGRPCUserInfo(self.config, ctx, self.ca_pool).Name
	return users.GetFavorites(self.config, user_name, in.Type)
}
