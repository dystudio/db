/*
  Copyright (c) 2012-2013 José Carlos Nieto, http://xiam.menteslibres.org/

  Permission is hereby granted, free of charge, to any person obtaining
  a copy of this software and associated documentation files (the
  "Software"), to deal in the Software without restriction, including
  without limitation the rights to use, copy, modify, merge, publish,
  distribute, sublicense, and/or sell copies of the Software, and to
  permit persons to whom the Software is furnished to do so, subject to
  the following conditions:

  The above copyright notice and this permission notice shall be
  included in all copies or substantial portions of the Software.

  THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
  EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
  MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
  NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
  LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
  OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
  WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package mongo

import (
	"fmt"
	"github.com/gosexy/db"
	"github.com/gosexy/sugar"
	"github.com/gosexy/to"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
	"reflect"
	"regexp"
	"strings"
)

// Mongodb Collection
type SourceCollection struct {
	name       string
	parent     *Source
	collection *mgo.Collection
}

/*
	Returns the collection name as a string.
*/
func (self *SourceCollection) Name() string {
	return self.name
}

/*
	Fetches a result delimited by terms into a pointer to map or struct given by
	dst.
*/
func (self *SourceCollection) Fetch(dst interface{}, terms ...interface{}) error {
	return nil
}

/*
	Fetches results delimited by terms into an slice of maps or structs given by
	the pointer dst.
*/
func (self *SourceCollection) FetchAll(dst interface{}, terms ...interface{}) error {
	return nil
}

// Transforms conditions into something *mgo.Session can understand.
func marshal(where db.Cond) map[string]interface{} {
	conds := make(map[string]interface{})

	for key, val := range where {
		chunks := strings.Split(strings.Trim(key, " "), " ")

		if len(chunks) >= 2 {
			op := ""
			switch chunks[1] {
			case ">":
				op = "$gt"
			case "<":
				op = "$gt"
			case "<=":
				op = "$lte"
			case ">=":
				op = "$gte"
			default:
				op = chunks[1]
			}
			conds[chunks[0]] = map[string]interface{}{op: toInternal(val)}
		} else {
			conds[key] = toInternal(val)
		}

	}

	return conds
}

/*
	Deletes the whole collection.
*/
func (self *SourceCollection) Truncate() error {
	err := self.collection.DropCollection()

	if err != nil {
		return err
	}

	return nil
}

/*
	Returns true if the collection exists.
*/
func (self *SourceCollection) Exists() bool {
	query := self.parent.database.C("system.namespaces").Find(db.Item{"name": fmt.Sprintf("%s.%s", self.parent.Name(), self.Name())})
	count, _ := query.Count()
	if count > 0 {
		return true
	}
	return false
}

/*
	Appends items to the collection. An item could be either a map or a struct.
*/
func (self *SourceCollection) Append(items ...interface{}) ([]db.Id, error) {
	return nil, nil
}

// Compiles terms into something *mgo.Session can understand.
func (self *SourceCollection) compileConditions(term interface{}) interface{} {
	switch term.(type) {
	case []interface{}:
		values := []interface{}{}
		itop := len(term.([]interface{}))
		for i := 0; i < itop; i++ {
			value := self.compileConditions(term.([]interface{})[i])
			if value != nil {
				values = append(values, value)
			}
		}
		if len(values) > 0 {
			return values
		}
	case db.Or:
		values := []interface{}{}
		itop := len(term.(db.Or))
		for i := 0; i < itop; i++ {
			values = append(values, self.compileConditions(term.(db.Or)[i]))
		}
		condition := map[string]interface{}{"$or": values}
		return condition
	case db.And:
		values := []interface{}{}
		itop := len(term.(db.And))
		for i := 0; i < itop; i++ {
			values = append(values, self.compileConditions(term.(db.And)[i]))
		}
		condition := map[string]interface{}{"$and": values}
		return condition
	case db.Cond:
		return marshal(term.(db.Cond))
	}
	return nil
}

// Compiles terms into something that *mgo.Session can understand.
func (self *SourceCollection) compileQuery(terms []interface{}) interface{} {
	var query interface{}

	compiled := self.compileConditions(terms)

	if compiled != nil {
		conditions := compiled.([]interface{})
		if len(conditions) == 1 {
			query = conditions[0]
		} else {
			// this should be correct.
			// query = map[string]interface{}{"$and": conditions}

			// trying to workaround https://jira.mongodb.org/browse/SERVER-4572
			mapped := map[string]interface{}{}
			for _, v := range conditions {
				for kk, _ := range v.(map[string]interface{}) {
					mapped[kk] = v.(map[string]interface{})[kk]
				}
			}

			query = mapped
		}
	} else {
		query = map[string]interface{}{}
	}

	return query
}

// Removes all the items that match the given conditions.
func (self *SourceCollection) Remove(terms ...interface{}) error {

	query := self.compileQuery(terms)

	_, err := self.collection.RemoveAll(query)

	return err
}

// Updates all the items that match the given conditions.
func (self *SourceCollection) Update(terms ...interface{}) error {

	var set interface{}
	var upsert interface{}
	var modify interface{}

	set = nil
	upsert = nil
	modify = nil

	query := self.compileQuery(terms)

	itop := len(terms)

	for i := 0; i < itop; i++ {
		term := terms[i]

		switch term.(type) {
		case db.Set:
			set = term.(db.Set)
		case db.Upsert:
			upsert = term.(db.Upsert)
		case db.Modify:
			modify = term.(db.Modify)
		}
	}

	var err error

	if set != nil {
		_, err = self.collection.UpdateAll(query, db.Item{"$set": set})
		return err
	}

	if modify != nil {
		_, err = self.collection.UpdateAll(query, modify)
		return err
	}

	if upsert != nil {
		_, err = self.collection.Upsert(query, upsert)
		return err
	}

	return nil
}

// Calls a SourceCollection function by name.
func (self *SourceCollection) invoke(fn string, terms []interface{}) []reflect.Value {

	reflected := reflect.TypeOf(self)

	method, _ := reflected.MethodByName(fn)

	args := make([]reflect.Value, 1+len(terms))

	args[0] = reflect.ValueOf(self)

	itop := len(terms)
	for i := 0; i < itop; i++ {
		args[i+1] = reflect.ValueOf(terms[i])
	}

	exec := method.Func.Call(args)

	return exec
}

// Returns the number of items that match the given conditions.
func (self *SourceCollection) Count(terms ...interface{}) (int, error) {
	q := self.invoke("BuildQuery", terms)

	p := q[0].Interface().(*mgo.Query)

	count, err := p.Count()

	return count, err
}

// Returns the first db.Item that matches the given conditions.
func (self *SourceCollection) Find(terms ...interface{}) db.Item {

	var item db.Item

	terms = append(terms, db.Limit(1))

	result := self.invoke("FindAll", terms)

	if len(result) > 0 {
		response := result[0].Interface().([]db.Item)
		if len(response) > 0 {
			item = response[0]
		}
	}

	return item
}

// Returns a mgo.Query based on the given terms.
func (self *SourceCollection) BuildQuery(terms ...interface{}) *mgo.Query {

	var sort interface{}

	limit := -1
	offset := -1
	sort = nil

	// Conditions
	query := self.compileQuery(terms)

	itop := len(terms)
	for i := 0; i < itop; i++ {
		term := terms[i]

		switch term.(type) {
		case db.Limit:
			limit = int(term.(db.Limit))
		case db.Offset:
			offset = int(term.(db.Offset))
		case db.Sort:
			sort = term.(db.Sort)
		}
	}

	// Actually executing query, returning a pointer.
	// fmt.Printf("actual: %v\n", query)
	q := self.collection.Find(query)

	// Applying limits and offsets.
	if offset > -1 {
		q = q.Skip(offset)
	}

	if limit > -1 {
		q = q.Limit(limit)
	}

	// Sorting result
	if sort != nil {
		for key, val := range sort.(db.Sort) {
			sval := to.String(val)
			if sval == "-1" || sval == "DESC" {
				q = q.Sort("-" + key)
			} else if sval == "1" || sval == "ASC" {
				q = q.Sort(key)
			} else {
				panic(fmt.Sprintf(`Unknown sort value "%s".`, sval))
			}
		}
	}

	return q
}

// Transforms data from db.Item format into mgo format.
func toInternal(val interface{}) interface{} {

	switch val.(type) {
	case []db.Id:
		ids := make([]bson.ObjectId, len(val.([]db.Id)))
		for i, _ := range val.([]db.Id) {
			ids[i] = bson.ObjectIdHex(string(val.([]db.Id)[i]))
		}
		return ids
	case db.Id:
		return bson.ObjectIdHex(string(val.(db.Id)))
	case db.Item:
		for k, _ := range val.(db.Item) {
			val.(db.Item)[k] = toInternal(val.(db.Item)[k])
		}
	case db.Cond:
		for k, _ := range val.(db.Cond) {
			val.(db.Cond)[k] = toInternal(val.(db.Cond)[k])
		}
	case map[string]interface{}:
		for k, _ := range val.(map[string]interface{}) {
			val.(map[string]interface{})[k] = toInternal(val.(map[string]interface{})[k])
		}
	}

	return val
}

// Transforms data from mgo format into db.Item format.
func toNative(val interface{}) interface{} {

	switch val.(type) {
	case bson.M:
		v2 := map[string]interface{}{}
		for k, v := range val.(bson.M) {
			v2[k] = toNative(v)
		}
		return v2
	case bson.ObjectId:
		return db.Id(val.(bson.ObjectId).Hex())
	}

	return val

}

// Returns all the items that match the given conditions. See Find().
func (self *SourceCollection) FindAll(terms ...interface{}) []db.Item {
	var items []db.Item
	var result []interface{}

	var relate interface{}
	var relateAll interface{}

	var itop int

	// Analyzing
	itop = len(terms)

	for i := 0; i < itop; i++ {
		term := terms[i]

		switch term.(type) {
		case db.Relate:
			relate = term.(db.Relate)
		case db.RelateAll:
			relateAll = term.(db.RelateAll)
		}
	}

	// Retrieving data
	q := self.invoke("BuildQuery", terms)

	p := q[0].Interface().(*mgo.Query)

	p.All(&result)

	var relations []sugar.Map

	// This query is related to other collections.
	if relate != nil {
		for rname, rterms := range relate.(db.Relate) {
			rcollection, _ := self.parent.Collection(rname)

			ttop := len(rterms)
			for t := ttop - 1; t >= 0; t-- {
				rterm := rterms[t]
				switch rterm.(type) {
				case db.Collection:
					rcollection = rterm.(db.Collection)
				}
			}

			relations = append(relations, sugar.Map{"all": false, "name": rname, "collection": rcollection, "terms": rterms})
		}
	}

	if relateAll != nil {
		for rname, rterms := range relateAll.(db.RelateAll) {
			rcollection, _ := self.parent.Collection(rname)

			ttop := len(rterms)
			for t := ttop - 1; t >= 0; t-- {
				rterm := rterms[t]
				switch rterm.(type) {
				case db.Collection:
					rcollection = rterm.(db.Collection)
				}
			}

			relations = append(relations, sugar.Map{"all": true, "name": rname, "collection": rcollection, "terms": rterms})
		}
	}

	var term interface{}

	jtop := len(relations)

	itop = len(result)
	items = make([]db.Item, itop)

	for i := 0; i < itop; i++ {

		item := db.Item{}

		// Default values.
		for key, val := range result[i].(bson.M) {
			item[key] = toNative(val)
		}

		// Querying relations
		for j := 0; j < jtop; j++ {

			relation := relations[j]

			terms := []interface{}{}

			ktop := len(relation["terms"].(db.On))

			for k := 0; k < ktop; k++ {

				//term = tcopy[k]
				term = relation["terms"].(db.On)[k]

				switch term.(type) {
				// Just waiting for db.Cond statements.
				case db.Cond:
					for wkey, wval := range term.(db.Cond) {
						//if reflect.TypeOf(wval).Kind() == reflect.String { // does not always work.
						if reflect.TypeOf(wval).Name() == "string" {
							// Matching dynamic values.
							matched, _ := regexp.MatchString("\\{.+\\}", wval.(string))
							if matched {
								// Replacing dynamic values.
								kname := strings.Trim(wval.(string), "{}")
								term = db.Cond{wkey: item[kname]}
							}
						}
					}
				}
				terms = append(terms, term)
			}

			// Executing external query.
			if relation["all"] == true {
				value := relation["collection"].(*SourceCollection).invoke("FindAll", terms)
				item[relation["name"].(string)] = value[0].Interface().([]db.Item)
			} else {
				value := relation["collection"].(*SourceCollection).invoke("Find", terms)
				item[relation["name"].(string)] = value[0].Interface().(db.Item)
			}

		}

		// Appending to results.
		items[i] = item
	}

	return items
}
