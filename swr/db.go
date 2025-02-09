/*  Star Wars Role-Playing Mud
 *  Copyright (C) 2022 @{See Authors}
 *
 *  This program is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  This program is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *  GNU General Public License for more details.
 *
 *  You should have received a copy of the GNU General Public License
 *  along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 */
package swr

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var _db *GameDatabase

type HelpData struct {
	Name     string   `yaml:"name"`
	Keywords []string `yaml:"keywords,flow"`
	Desc     string   `yaml:"desc"`
	Level    uint     `yaml:"level"`
}

type GameDatabase struct {
	m               *sync.Mutex
	db              *gorm.DB
	clients         []Client
	entities        []Entity
	areas           map[string]*AreaData // pointers to the [AreaData] of the game.
	rooms           map[uint]*RoomData   // pointers to the room structs in [AreaData]
	mobs            map[uint]*CharData   // used as templates for spawning [entities]
	items           map[uint]*ItemData   // used as templates for spawning [items]
	ships           []Ship
	ship_prototypes map[uint]*ShipData // used as templates for spawning [ships]
	starsystems     []Starsystem       // Planets (star systems)
	helps           []*HelpData
}

// TODO: Flesh out the interfaces and wrap the GameDatabase behind this...
type Database interface {
	SaveArea(area *AreaData) error
	SaveEntity(entity Entity) error
	SaveObject(object Item) error
	SaveShip(ship Ship) error
}

func DB() *GameDatabase {
	if _db == nil {
		log.Printf("Starting Database.")
		db, e := gorm.Open(sqlite.Open("data/game.db"), &gorm.Config{})
		ErrorCheck(e)
		db.AutoMigrate(&Account{})
		_db = new(GameDatabase)
		_db.m = &sync.Mutex{}
		_db.db = db
		_db.clients = make([]Client, 0, 64)
		_db.entities = make([]Entity, 0)
		_db.areas = make(map[string]*AreaData)
		_db.rooms = make(map[uint]*RoomData)
		_db.mobs = make(map[uint]*CharData)
		_db.items = make(map[uint]*ItemData)
		_db.ships = make([]Ship, 0)
		_db.ship_prototypes = make(map[uint]*ShipData)
		_db.starsystems = make([]Starsystem, 0)
		_db.helps = make([]*HelpData, 0)
		log.Printf("Database Started.")
	}
	return _db
}

func (d *GameDatabase) Lock() {
	d.m.Lock()
}

func (d *GameDatabase) Unlock() {
	d.m.Unlock()
}

func (d *GameDatabase) AddClient(client Client) {
	d.Lock()
	defer d.Unlock()
	d.clients = append(d.clients, client)
}

func (d *GameDatabase) RemoveClient(client Client) {
	d.Lock()
	defer d.Unlock()
	for _, e := range d.entities {
		if e == nil {
			continue
		}
		if e.IsPlayer() {
			p := e.(*PlayerProfile)
			if p.Client != nil {
				if p.Client == client {
					d.RemoveEntity(e)
				}
			}
		}
	}
	index := -1
	for i, c := range d.clients {
		if c == nil {
			continue
		}
		if c.GetId() == client.GetId() {
			index = i
		}
	}
	if index > -1 {
		ret := make([]Client, len(d.clients)-1)
		ret = append(ret, d.clients[:index]...)
		ret = append(ret, d.clients[index+1:]...)
		d.clients = ret
	}
}

func (d *GameDatabase) RemoveEntity(entity Entity) {
	d.Lock()
	defer d.Unlock()
	if entity == nil {
		return
	}
	index := -1
	for i, e := range d.entities {
		if e == nil {
			continue
		}
		if e == entity {
			index = i
		}
	}
	if index > -1 {
		ret := make([]Entity, len(d.entities)-1)
		ret = append(ret, d.entities[:index]...)
		ret = append(ret, d.entities[index+1:]...)
		d.entities = ret
	} else {
		ErrorCheck(Err(fmt.Sprintf("Can't find entity %s to remove.", entity.GetCharData().Name)))
	}
}
func (d *GameDatabase) RemoveShip(ship Ship) {
	d.Lock()
	defer d.Unlock()
	index := -1
	for i, s := range d.ships {
		if s == nil {
			continue
		}
		if s == ship {
			index = i
		}
	}
	if index > -1 {
		ret := make([]Ship, len(d.ships)-1)
		ret = append(ret, d.ships[:index]...)
		ret = append(ret, d.ships[index+1:]...)
		d.ships = ret
	} else {
		ErrorCheck(Err(fmt.Sprintf("Can't find ship %s", ship.GetData().Name)))
	}
}
func (d *GameDatabase) RemoveShipPrototype(ship Ship) {
	d.Lock()
	defer d.Unlock()
	if ship == nil {
		return
	}
	delete(d.ship_prototypes, ship.GetData().OId)
}

func (d *GameDatabase) RemoveArea(area *AreaData) {
	d.Lock()
	defer d.Unlock()
	if a, ok := d.areas[area.Name]; ok {
		for _, r := range a.Rooms {
			delete(d.rooms, r.Id)
		}
	}
}

// The Mother of all load functions
func (d *GameDatabase) Load() {

	log.Printf("Loading Database...")
	// Load Help files
	d.LoadHelps()

	// Load Areas
	d.LoadAreas()

	// Load Items
	d.LoadItems()

	// Load Planets / Star Systems
	d.LoadPlanets()

	// Load Mobs
	d.LoadMobs()

	// Load Ships
	d.LoadShips()

}

func (d *GameDatabase) LoadHelps() {
	log.Print("Loading help files.")
	flist, err := os.ReadDir("docs")
	ErrorCheck(err)
	d.Lock()
	defer d.Unlock()
	for _, help_file := range flist {
		fpath := fmt.Sprintf("docs/%s", help_file.Name())
		fp, err := os.ReadFile(fpath)
		ErrorCheck(err)
		help := new(HelpData)
		err = yaml.Unmarshal(fp, help)
		ErrorCheck(err)
		d.helps = append(d.helps, help)
	}
	log.Printf("%d help files loaded.\n", len(flist))
}

func (d *GameDatabase) LoadAreas() {
	log.Print("Loading area files.")
	flist, err := os.ReadDir("data/areas")
	ErrorCheck(err)
	count := 0
	for _, area_file := range flist {
		if strings.HasSuffix(area_file.Name(), "yml") {
			d.LoadArea(area_file.Name())
			count++
		}
	}
	log.Printf("%d areas loaded.\n", count)
}

func (d *GameDatabase) LoadArea(name string) {
	fpath := fmt.Sprintf("data/areas/%s", name)
	fp, err := os.ReadFile(fpath)
	ErrorCheck(err)
	area := new(AreaData)
	err = yaml.Unmarshal(fp, area)
	ErrorCheck(err)
	d.Lock()
	defer d.Unlock()
	for i := range area.Rooms {
		room := area.Rooms[i]
		room.Area = area
		d.rooms[room.Id] = &room
		time.Sleep(1 * time.Millisecond)
	}
	d.areas[area.Name] = area
}

func (d *GameDatabase) LoadPlanets() {
	log.Printf("Loading planet files.")
	flist, err := os.ReadDir("data/planets")
	ErrorCheck(err)
	d.Lock()
	defer d.Unlock()
	for _, f := range flist {
		fpath := fmt.Sprintf("data/planets/%s", f.Name())
		fp, err := os.ReadFile(fpath)
		ErrorCheck(err)
		p := new(StarSystemData)
		err = yaml.Unmarshal(fp, p)
		ErrorCheck(err)
		d.starsystems = append(d.starsystems, p)
	}
	log.Printf("%d total planets loaded.", len(d.starsystems))
}

func (d *GameDatabase) LoadItems() {
	log.Print("Loading item files.")
	err := filepath.Walk("data/items",
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
					d.LoadItem(path)
				}
			}
			return nil
		})
	ErrorCheck(err)
	log.Printf("%d items loaded.", len(d.items))
}

func (d *GameDatabase) LoadItem(path string) {
	fp, err := os.ReadFile(path)
	ErrorCheck(err)
	item := new(ItemData)
	err = yaml.Unmarshal(fp, item)
	ErrorCheck(err)
	item.Filename = path
	d.Lock()
	defer d.Unlock()
	d.items[item.Id] = item
}

func (d *GameDatabase) LoadMobs() {
	log.Print("Loading mob files.")
	err := filepath.Walk("data/mobs",
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
					d.LoadMob(path)
				}
			}
			return nil
		})
	ErrorCheck(err)
	log.Printf("%d mobs loaded.", len(d.mobs))
}
func (d *GameDatabase) LoadMob(path string) {
	fp, err := os.ReadFile(path)
	ErrorCheck(err)
	ch := new(CharData)
	err = yaml.Unmarshal(fp, ch)
	ErrorCheck(err)
	ch.Filename = path
	d.Lock()
	defer d.Unlock()
	d.mobs[ch.Id] = ch
}
func (d *GameDatabase) LoadShips() {
	log.Print("Loading ship files.")
	err := filepath.Walk("data/ships",
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				if strings.Contains(path, "prototype") {
					if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
						d.LoadShipPrototype(path)
					}
				} else {
					if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
						d.LoadShip(path)
					}
				}
			}
			return nil
		})
	ErrorCheck(err)
	log.Printf("%d ships loaded. %d prototypes.", len(d.ships), len(d.ship_prototypes))
}
func (d *GameDatabase) LoadShip(path string) {
	fp, err := os.ReadFile(path)
	ErrorCheck(err)
	ship := new(ShipData)
	err = yaml.Unmarshal(fp, ship)
	ErrorCheck(err)
	d.Lock()
	defer d.Unlock()
	d.ships = append(d.ships, ship)
}
func (d *GameDatabase) LoadShipPrototype(path string) {
	fp, err := os.ReadFile(path)
	ErrorCheck(err)
	ship := new(ShipData)
	err = yaml.Unmarshal(fp, ship)
	ErrorCheck(err)
	d.Lock()
	defer d.Unlock()
	d.ship_prototypes[ship.Id] = ship
}

// The Mother of all save functions
func (d *GameDatabase) Save() {
	d.Lock()
	defer d.Unlock()
	echo_all("\r\n&xSaving Game World&d\r\n")
	t := time.Now()
	d.SaveAreas()
	d.SaveMobs()
	d.SaveItems()
	d.SaveShips()
	d.SavePlayers()
	echo_all(sprintf("\r\n&xSave took %s&d\r\n", time.Since(t).String()))
}

func (d *GameDatabase) SaveAreas() {
	for _, area := range d.areas {
		d.SaveArea(area)
	}
}
func (d *GameDatabase) SaveItems() {
	for _, item := range d.items {
		d.SaveItem(item)
	}
}
func (d *GameDatabase) SaveMobs() {
	for _, mob := range d.mobs {
		d.SaveMob(mob)
	}
}

func (d *GameDatabase) SaveShips() {
	for _, ship := range d.ship_prototypes {
		buf, err := yaml.Marshal(ship)
		ErrorCheck(err)
		err = os.WriteFile(sprintf("data/ships/prototypes/%s.yml", strings.ToLower(strings.ReplaceAll(ship.Type, " ", "_"))), buf, 0755)
		ErrorCheck(err)
	}
	for _, ship := range d.ships {
		d.SaveShip(ship)
	}
}

func (d *GameDatabase) SavePlayers() {
	// lets compact memory while we are at it...
	el := make([]Entity, 0)
	for _, e := range d.entities {
		if e == nil {
			continue // ignore nil Entities (dead, removed, empty memory)
		}
		if e.IsPlayer() {
			player := e.(*PlayerProfile)
			d.SavePlayerData(player)
		}
		el = append(el, e)
	}
	// remove old slice with new slice... like a garbage collector.
	d.entities = el
}

func (d *GameDatabase) SaveShip(ship Ship) {
	buf, err := yaml.Marshal(ship)
	ErrorCheck(err)
	err = os.WriteFile(sprintf("data/ships/%s.yml", strings.ToLower(strings.ReplaceAll(ship.GetData().Name, " ", "_"))), buf, 0755)
	ErrorCheck(err)
}

func (d *GameDatabase) SaveArea(area *AreaData) {
	buf, err := yaml.Marshal(area)
	ErrorCheck(err)
	err = os.WriteFile(sprintf("data/areas/%s.yml", area.Name), buf, 0755)
	for _, m := range area.Mobs {
		mob := d.mobs[m.Mob]
		d.SaveMob(mob)
	}
	for _, i := range area.Items {
		item := d.items[i.Item]
		d.SaveItem(item)
	}
	ErrorCheck(err)
}
func (d *GameDatabase) SaveItem(item *ItemData) {
	buf, err := yaml.Marshal(item)
	ErrorCheck(err)
	dir := filepath.Dir(item.Filename)
	os.MkdirAll(dir, 0755)
	err = os.WriteFile(item.Filename, buf, 0755)
	ErrorCheck(err)
}
func (d *GameDatabase) SaveMob(mob *CharData) {
	buf, err := yaml.Marshal(mob)
	ErrorCheck(err)
	dir := filepath.Dir(mob.Filename)
	os.MkdirAll(dir, 0755)
	err = os.WriteFile(mob.Filename, buf, 0755)
	ErrorCheck(err)
}

func (d *GameDatabase) GetPlayer(name string) *PlayerProfile {
	d.Lock()
	defer d.Unlock()
	var player *PlayerProfile
	for i := range d.entities {
		e := d.entities[i]
		if e != nil {
			if e.IsPlayer() {
				if e.GetCharData().Name == name {
					player = e.(*PlayerProfile)
				}
			}
		}
	}
	d.Unlock() // unlock early
	// Player isn't online
	if player == nil {
		path := fmt.Sprintf("data/accounts/%s/%s.yml", strings.ToLower(name[0:1]), strings.ToLower(name))
		player = d.ReadPlayerData(path)
	}
	return player
}

func (d *GameDatabase) ReadPlayerData(filename string) *PlayerProfile {
	fp, err := os.ReadFile(filename)
	ErrorCheck(err)
	p_data := new(PlayerProfile)
	err = yaml.Unmarshal(fp, p_data)
	if err != nil {
		ErrorCheck(err)
		return nil
	}
	if p_data.Char.Equipment == nil {
		p_data.Char.Equipment = make(map[string]*ItemData)
	}
	if p_data.Char.Inventory == nil {
		p_data.Char.Inventory = make([]*ItemData, 0)
	}
	return p_data
}

func (d *GameDatabase) SavePlayerData(player *PlayerProfile) {
	name := strings.ToLower(player.Char.Name)
	filename := fmt.Sprintf("data/accounts/%s/%s.yml", name[0:1], name)
	buf, err := yaml.Marshal(player)
	ErrorCheck(err)
	err = os.WriteFile(filename, buf, 0755)
	ErrorCheck(err)
}

func (d *GameDatabase) GetPlayerEntityByName(name string) Entity {
	d.Lock()
	defer d.Unlock()
	for _, e := range d.entities {
		if e == nil {
			continue
		}
		if e.IsPlayer() {
			p := e.(*PlayerProfile)
			if strings.EqualFold(p.Char.Name, name) {
				return p
			}
		}
	}
	return nil
}
func (d *GameDatabase) ReadCharData(filename string) *CharData {
	fp, err := os.ReadFile(filename)
	ErrorCheck(err)
	char_data := new(CharData)
	yaml.Unmarshal(fp, char_data)
	return char_data
}

func (d *GameDatabase) SaveCharData(char_data *CharData, filename string) {
	buf, err := yaml.Marshal(char_data)
	ErrorCheck(err)
	err = os.WriteFile(filename, buf, 0755)
	ErrorCheck(err)
}

func (d *GameDatabase) AddEntity(entity Entity) {
	d.Lock()
	defer d.Unlock()
	d.entities = append(d.entities, entity)
}

func (d *GameDatabase) AddShip(ship Ship) {
	d.Lock()
	defer d.Unlock()
	d.ships = append(d.ships, ship)
}

func (d *GameDatabase) SpawnEntity(entity Entity) Entity {
	e := entity_clone(entity)
	e.GetCharData().State = ENTITY_STATE_NORMAL
	if e.GetCharData().AI == nil {
		e.GetCharData().AI = MakeGenericBrain(e)
		e.GetCharData().AI.OnSpawn()
	}
	d.AddEntity(e)
	return e
}

func (d *GameDatabase) SpawnShip(ship Ship) Ship {
	s := ship_clone(ship)
	d.AddShip(s)
	return s
}

func (d *GameDatabase) GetShip(shipId uint) Ship {
	d.Lock()
	defer d.Unlock()
	for _, ship := range d.ships {
		if ship.GetData().Id == shipId {
			return ship
		}
	}
	return nil
}
func (d *GameDatabase) ShipNameAvailable(name string) bool {
	d.Lock()
	defer d.Unlock()
	for _, ship := range d.ships {
		if strings.EqualFold(ship.GetData().Name, name) {
			return false
		}
	}
	return true
}
func (d *GameDatabase) GetShipsInSystem(system string) []Ship {
	ret := make([]Ship, 0)
	d.Lock()
	defer d.Unlock()
	for _, ship := range d.ships {
		s := ship.GetData()
		if s.CurrentSystem == system {
			if s.InSpace {
				ret = append(ret, ship)
			}
		}
	}
	return ret
}
func (d *GameDatabase) GetShipsInRoom(roomId uint) []Ship {
	d.Lock()
	defer d.Unlock()
	ret := make([]Ship, 0)
	for _, s := range d.ships {
		if s.GetData().LocationId == roomId && !s.GetData().InSpace {
			ret = append(ret, s)
		}
	}
	return ret
}
func (d *GameDatabase) GetEntity(entity Entity) Entity {
	d.Lock()
	defer d.Unlock()
	for _, e := range d.entities {
		if e == entity {
			return e
		}
	}
	return nil
}
func (d *GameDatabase) GetEntitiesInRoom(roomId uint, shipId uint) []Entity {
	d.Lock()
	defer d.Unlock()
	ret := make([]Entity, 0)
	for _, entity := range d.entities {
		if entity == nil {
			continue
		}
		if entity.RoomId() == roomId && entity.ShipId() == shipId {
			ret = append(ret, entity)
		}
	}
	return ret
}

func (d *GameDatabase) GetRoom(roomId uint, shipId uint) *RoomData {
	d.Lock()
	defer d.Unlock()
	if shipId > 0 {
		for _, s := range d.ships {
			if s.GetData().Id == shipId {
				for _, r := range s.GetData().Rooms {
					if r.Id == roomId {
						return r
					}
				}
			}
		}
	} else {
		for _, r := range d.rooms {
			if r == nil {
				continue
			}
			if r.Id == roomId {
				return r
			}
		}
	}
	return nil
}

func (d *GameDatabase) GetNextRoomVnum(roomId uint, shipId uint) uint {
	d.Lock()
	defer d.Unlock()
	if shipId > 0 {
		for _, s := range d.ships {
			if s.GetData().Id == shipId {
				lastVnum := uint(0)
				for _, r := range s.GetData().Rooms {
					if r.Name == "A void" { // return the first void prototype room...
						return r.Id
					}
					if r.Id > lastVnum {
						lastVnum = r.Id
					}
				}
				return lastVnum + 1
			}
		}
	} else {
		lastVnum := uint(0)
		// let's get the room's area
		var room *RoomData
		if r, ok := d.rooms[roomId]; ok {
			room = r
		}
		if room == nil {
			return 0
		}
		for _, r := range room.Area.Rooms {
			if r.Name == "A void" { // return the first void prototype room
				return r.Id
			}
			if r.Id > lastVnum {
				lastVnum = r.Id
			}
		}
		return lastVnum + 1
	}
	return 0
}
func (d *GameDatabase) GetNextItemVnum() uint {
	d.Lock()
	defer d.Unlock()
	for i := uint(1); i < (^uint(0)); i++ {
		if _, ok := d.items[i]; !ok {
			return i
		}
	}
	return 0
}
func (d *GameDatabase) GetNextMobVnum() uint {
	d.Lock()
	defer d.Unlock()
	for i := uint(1); i < (^uint(0)); i++ {
		if _, ok := d.mobs[i]; !ok {
			return i
		}
	}
	return 0
}
func (d *GameDatabase) GetNextShipVnum() uint {
	d.Lock()
	defer d.Unlock()
	for i := uint(1); i < (^uint(0)); i++ {
		if _, ok := d.ship_prototypes[i]; !ok {
			return i
		}
	}
	return 0
}

func (d *GameDatabase) GetItem(itemId uint) Item {
	d.Lock()
	defer d.Unlock()
	for _, i := range d.items {
		if i == nil {
			continue
		}
		if i.GetId() == itemId {
			return i
		}
	}
	return nil
}

func (d *GameDatabase) GetMob(mobId uint) Entity {
	d.Lock()
	defer d.Unlock()
	if m, ok := d.mobs[mobId]; ok {
		return m
	}
	return nil
}

func (d *GameDatabase) GetEntityForClient(client Client) Entity {
	d.Lock()
	defer d.Unlock()
	for _, e := range d.entities {
		if e == nil {
			continue
		}
		if e.IsPlayer() {
			player := e.(*PlayerProfile)
			if player.Client == client {
				return player
			}
		}
	}
	return nil
}

func (d *GameDatabase) GetHelp(help string) []*HelpData {
	d.Lock()
	defer d.Unlock()
	ret := []*HelpData{}
	for _, h := range d.helps {
		for _, keyword := range h.Keywords {
			if len(keyword) < len(help) {
				continue
			}
			match := true
			for i, r := range help {
				if keyword[i] != byte(r) {
					match = false
				}
			}
			if match {
				ret = append(ret, h)
			}
		}
	}
	return ret
}

func (d *GameDatabase) SetRoom(id uint, room *RoomData) {
	d.Lock()
	defer d.Unlock()
	d.rooms[id] = room
}

func (d *GameDatabase) ResetAll() {
	for area_name, area := range d.areas {
		log.Printf("Resetting Area %s", area_name)
		area_reset(area)
	}
}

func echo_all(msg string) {
	for _, c := range DB().clients {
		c.Send(msg)
	}
}
