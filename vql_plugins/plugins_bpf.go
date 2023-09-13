//go:build linux && linuxbpf

/*
   Velociraptor - Dig Deeper
   Copyright (C) 2019-2022 Rapid7 Inc.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published
   by the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/
package plugins

import (
	_ "www.velocidex.com/golang/velociraptor/vql/linux/tcpsnoop"
	_ "www.velocidex.com/golang/velociraptor/vql/linux/dnssnoop"
	_ "www.velocidex.com/golang/velociraptor/vql/linux/chattrsnoop"
)
