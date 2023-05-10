package builder

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

var builderPool = sync.Pool{
	New: func() interface{} {
		return new(SQLBulder)._default()
	},
}

type SQLBulder struct {
	tableName      string
	action         SQLAction
	fields         []string
	args           []interface{}
	whereList      Wheres
	orders         Orders
	groups         Groups
	limitSize      Limit
	offsetSize     Offset
	parameterized  bool
	delimiter      string
	values         Values
	forceIndexName ForceIndex
	insert         *Insert
	reuse          bool
}

type Insert struct {
	onDupKeyUpdates onDupKeyUpdates
	ignore          bool
}

type UpdateRaw struct {
	Expr string
	Args []interface{}
}

type onDupKeyUpdates []string

func (keys onDupKeyUpdates) String() string {
	if len(keys) == 0 {
		return ""
	}
	fieldUpdateSb := strings.Builder{}
	fieldUpdateSb.WriteString("ON DUPLICATE KEY UPDATE ")
	for i, key := range keys {
		fieldUpdateSb.WriteString(key)
		fieldUpdateSb.WriteString(" = VALUES(")
		fieldUpdateSb.WriteString(key)
		if i == len(keys)-1 {
			fieldUpdateSb.WriteString(")")
		} else {
			fieldUpdateSb.WriteString("), ")
		}
	}
	return fieldUpdateSb.String()
}

type ForceIndex string

func (f ForceIndex) String() string {
	if f != "" {
		return "FORCE INDEX(" + string(f) + ")"
	}
	return ""
}

type Values []map[string]interface{}

type SQLAction string

const (
	SQLActionSelect SQLAction = "select"
	SQLActionUpdate SQLAction = "update"
	SQLActionDelete SQLAction = "delete"
	SQLActionInsert SQLAction = "insert"
)

type Groups []string

func (groups Groups) String() string {
	if len(groups) == 0 {
		return ""
	}
	return "GROUP BY " + strings.Join(groups, ",")
}

type Order struct {
	Order OrderEnum
	Field string
}

func (o *Order) String() string {
	return o.Field + " " + string(o.Order)
}

type Orders []*Order

func (orders Orders) String() string {
	if len(orders) == 0 {
		return ""
	}
	orderBySb := strings.Builder{}
	orderBySb.WriteString("ORDER BY ")
	orderBySb.WriteString(orders[0].String())
	for i := 1; i < len(orders); i++ {
		orderBySb.WriteString(",")
		orderBySb.WriteString(orders[i].String())
	}
	return orderBySb.String()
}

type OrderEnum string

const (
	OrderAsc  OrderEnum = "ASC"
	OrderDesc OrderEnum = "DESC"
)

type Limit int64

func (l Limit) String() string {
	if l == -1 {
		return ""
	}
	return "LIMIT " + strconv.FormatInt(int64(l), 10)
}

type Offset int64

func (o Offset) String() string {
	if o == -1 {
		return ""
	}
	return "OFFSET " + strconv.FormatInt(int64(o), 10)
}

type Operation string

const (
	OpEq    Operation = "="
	OpLt    Operation = "<"
	OpLte   Operation = "<="
	OpGt    Operation = ">"
	OpGte   Operation = ">="
	OpIn    Operation = "in"
	OpNotIn Operation = "not in"
)

func (o Operation) lower() Operation {
	return Operation(strings.ToLower(string(o)))
}

type WhereCond string

const (
	WhereCondAnd WhereCond = "AND"
	WhereCondOr  WhereCond = "OR"
)

type Where struct {
	Field        string
	Operation    Operation
	Value        []interface{}
	Children     Wheres
	Cond         WhereCond
	CombineRight *Where
	CombineLeft  *Where
}

// ((1,2),(3,4)) => map[field]{1,3}
func (wh *Where) CombineValues() (CombineWheres, map[string][]interface{}) {
	vals := map[string][]interface{}{wh.Field: wh.Value}
	whs := map[string]*Where{wh.Field: wh}
	leftCur := wh
	for leftCur.CombineLeft != nil {
		leftCur = leftCur.CombineLeft
		vals[leftCur.Field] = leftCur.Value
		whs[leftCur.Field] = leftCur
	}
	rightCur := wh
	for rightCur.CombineRight != nil {
		rightCur = rightCur.CombineRight
		vals[rightCur.Field] = rightCur.Value
		whs[rightCur.Field] = rightCur
	}
	return whs, vals
}

func (wh *Where) IsCombine() bool {
	return wh.CombineLeft != nil || wh.CombineRight != nil
}

type CombineWheres map[string]*Where

func (cw CombineWheres) SetValueByGivenFieldValues(field string, values []interface{}, all map[string][]interface{}) {
	fieldVals := all[field]
	fieldValsMap := make(map[interface{}][]int)
	for i, v := range fieldVals {
		fieldValsMap[v] = append(fieldValsMap[v], i)
	}
	needIndexs := make([]int, 0, len(values))
	for _, v := range values {
		needIndexs = append(needIndexs, fieldValsMap[v]...)
	}
	for f, vals := range all {
		if f == field {
			cw[field].Value = values
		} else {
			tmpValues := make([]interface{}, 0, len(needIndexs))
			for _, i := range needIndexs {
				tmpValues = append(tmpValues, vals[i])
			}
			cw[f].Value = tmpValues
		}
	}
}

func (wh *Where) Wheres(fn GetWhereFn) *Where {
	wh.Children = fn(wh.Children)
	return wh
}

func (wh *Where) string(first, needBracket bool, getPh func(typeKind reflect.Kind) string) (string, []interface{}) {
	if len(wh.Children) > 0 {
		s, args := wh.Children.string(needBracket, getPh)
		if first {
			return s, args
		}
		return string(wh.Cond) + " " + s, args
	}
	ph := getPh(reflect.TypeOf(wh.Value[0]).Kind())
	phs := ph
	field := wh.Field
	values := wh.Value

	if wh.Operation == OpIn || wh.Operation == OpNotIn {
		if wh.CombineRight == nil {
			phs = _wrapBracket(_stringRepeatJoin(ph, ",", len(wh.Value)))
		} else {
			combineFields := []string{}
			cur := wh
			allValues := make([][]interface{}, 0)
			phsSb := strings.Builder{}
			for cur != nil {
				combineFields = append(combineFields, cur.Field)
				phsSb.WriteString(getPh(reflect.TypeOf(cur.Value[0]).Kind()))
				allValues = append(allValues, cur.Value)
				cur = cur.CombineRight
				if cur != nil {
					phsSb.WriteString(",")
				}
			}
			field = _wrapBracket(strings.Join(combineFields, ","))
			phs = _wrapBracket(_stringRepeatJoin(_wrapBracket(phsSb.String()), ",", len(wh.Value)))

			values = make([]interface{}, 0, len(allValues)*len(allValues[0]))
			for i := 0; i < len(allValues[0]); i++ {
				for j := 0; j < len(allValues); j++ {
					values = append(values, allValues[j][i])
				}
			}
		}
	}

	if first {
		return strings.Join([]string{field, string(wh.Operation), phs}, " "), values
	}
	return strings.Join([]string{string(wh.Cond), field, string(wh.Operation), phs}, " "), values
}

type Wheres []*Where

func (whs Wheres) findByField(field string) []*Where {
	if len(whs) == 0 {
		return []*Where{}
	}
	nextLevel := make([]*Where, 0, len(whs))
	nextLevel = append(nextLevel, whs...)
	res := make([]*Where, 0)
	for len(nextLevel) > 0 {
		tmpLevel := make([]*Where, 0)
		for _, wh := range nextLevel {
			if wh.Field == field {
				res = append(res, wh)
			}
			tmpLevel = append(tmpLevel, wh.Children...)
		}
		nextLevel = tmpLevel
	}
	return res
}

func (whs Wheres) Where(field string, operation Operation, value interface{}) Wheres {
	return whs.where(field, operation, []interface{}{value}, WhereCondAnd)
}

func (whs Wheres) WhereIn(field string, value []interface{}) Wheres {
	return whs.where(field, OpIn, value, WhereCondAnd)
}

func (whs Wheres) OrWhereIn(field string, value []interface{}) Wheres {
	return whs.where(field, OpIn, value, WhereCondOr)
}

func (whs Wheres) WhereNotIn(field string, value []interface{}) Wheres {
	return whs.where(field, OpNotIn, value, WhereCondAnd)
}

func (whs Wheres) OrWhereNotIn(field string, value []interface{}) Wheres {
	return whs.where(field, OpNotIn, value, WhereCondOr)
}

func (whs Wheres) WhereCombineIn(fields []string, values [][]interface{}) Wheres {
	fieldValues := make(map[string][]interface{}, len(fields))
	for i, field := range fields {
		for _, v := range values {
			fieldValues[field] = append(fieldValues[field], v[i])
		}
	}
	for i, field := range fields {
		whs = whs.where(field, OpIn, fieldValues[field], WhereCondAnd)
		if i > 0 {
			whs[len(whs)-i-1].CombineRight = whs[len(whs)-i]
			whs[len(whs)-i].CombineLeft = whs[len(whs)-i-1]
		}
	}
	return whs
}

func (whs Wheres) OrWhere(field string, operation Operation, value interface{}) Wheres {
	return whs.where(field, operation, []interface{}{value}, WhereCondOr)
}

type GetWhereFn func(w Wheres) Wheres

func (whs Wheres) where(field string, operation Operation, value []interface{}, cond WhereCond) Wheres {
	wh := &Where{
		Field:     field,
		Operation: operation,
		Value:     value,
		Cond:      cond,
	}
	whs = append(whs, wh)
	return whs
}

func (whs Wheres) string(needBracket bool, getPh func(typeKind reflect.Kind) string) (string, []interface{}) {
	if len(whs) == 0 {
		return "", make([]interface{}, 0)
	}
	if len(whs) == 1 {
		return whs[0].string(true, false, getPh)
	}
	resList := make([]string, 0, len(whs))
	whArgs := make([]interface{}, 0)
	for i := 0; i < len(whs); i++ {
		str, args := whs[i].string(i == 0, len(whs) > 1, getPh)
		resList = append(resList, str)
		whArgs = append(whArgs, args...)
		if whs[i].CombineRight == nil {
			continue
		}
		for i+1 < len(whs) && whs[i].CombineRight != nil {
			i++
		}
	}
	res := strings.Join(resList, " ")
	if needBracket {
		return _wrapBracket(res), whArgs
	}
	return res, whArgs
}

type NewBuilderOpt struct {
	Parameterized bool
	Reuse         bool
}

func New(table string, opts ...NewBuilderOpt) *SQLBulder {
	reuse := true
	parameterized := true
	if len(opts) > 0 {
		reuse = opts[0].Reuse
		parameterized = opts[0].Parameterized
	}
	b := newSQLBuilder(table, reuse)
	b.parameterized = parameterized
	return b
}

func newSQLBuilder(table string, reuse bool) *SQLBulder {
	var builder *SQLBulder
	if reuse {
		builder = builderPool.Get().(*SQLBulder)
	} else {
		builder = new(SQLBulder)._default()
	}
	builder.tableName = table
	return builder
}

func putBackBuilder(builder *SQLBulder) {
	if builder.reuse {
		builderPool.Put(builder._default())
	}
}

func (builder *SQLBulder) _default() *SQLBulder {
	builder.offsetSize = -1
	builder.limitSize = -1
	builder.delimiter = "`"
	builder.insert = new(Insert)
	builder.tableName = ""
	builder.action = ""
	builder.fields = make([]string, 0)
	builder.args = make([]interface{}, 0)
	builder.whereList = make(Wheres, 0)
	builder.orders = make(Orders, 0)
	builder.groups = make(Groups, 0)
	builder.parameterized = false
	builder.delimiter = "`"
	builder.values = make(Values, 0)
	builder.forceIndexName = ""
	builder.reuse = true
	return builder
}

func (builder *SQLBulder) Wheres(fn GetWhereFn) *SQLBulder {
	wh := &Where{
		Cond: WhereCondAnd,
	}
	builder.whereList = append(builder.whereList, wh.Wheres(fn))
	return builder
}

func (builder *SQLBulder) OrWheres(fn GetWhereFn) *SQLBulder {
	wh := &Where{
		Cond: WhereCondOr,
	}
	builder.whereList = append(builder.whereList, wh.Wheres(fn))
	return builder
}

func (builder *SQLBulder) Where(field string, operation Operation, value interface{}) *SQLBulder {
	builder.whereList = builder.whereList.Where(field, operation.lower(), value)
	return builder
}

func (builder *SQLBulder) OrWhere(field string, operation Operation, value interface{}) *SQLBulder {
	builder.whereList = builder.whereList.OrWhere(field, operation.lower(), value)
	return builder
}

func (builder *SQLBulder) WhereIn(field string, value ...interface{}) *SQLBulder {
	if len(value) == 0 {
		return builder
	}
	vals := _getInValues(value)
	if len(vals) == 0 {
		return builder
	}
	builder.whereList = builder.whereList.WhereIn(field, vals)
	return builder
}

func (builder *SQLBulder) OrWhereIn(field string, value ...interface{}) *SQLBulder {
	if len(value) == 0 {
		return builder
	}
	vals := _getInValues(value)
	if len(vals) == 0 {
		return builder
	}
	builder.whereList = builder.whereList.OrWhereIn(field, vals)
	return builder
}

func (builder *SQLBulder) WhereNotIn(field string, value ...interface{}) *SQLBulder {
	if len(value) == 0 {
		return builder
	}
	vals := _getInValues(value)
	if len(vals) == 0 {
		return builder
	}
	builder.whereList = builder.whereList.WhereNotIn(field, vals)
	return builder
}

func (builder *SQLBulder) OrWhereNotIn(field string, value ...interface{}) *SQLBulder {
	if len(value) == 0 {
		return builder
	}
	vals := _getInValues(value)
	if len(vals) == 0 {
		return builder
	}
	builder.whereList = builder.whereList.OrWhereNotIn(field, vals)
	return builder
}

func (builder *SQLBulder) WhereCombineIn(field []string, value [][]interface{}) *SQLBulder {
	if len(value) == 0 {
		return builder
	}
	builder.whereList = builder.whereList.WhereCombineIn(field, value)
	return builder
}

func (builder *SQLBulder) GetWheresByField(field string) []*Where {
	return builder.whereList.findByField(field)
}

func (builder *SQLBulder) GetValuesByField(field string) []interface{} {
	res := make([]interface{}, 0)
	for _, item := range builder.values {
		if v, ok := item[field]; ok {
			res = append(res, v)
		}
	}
	return res
}

func (builder *SQLBulder) Select(fields ...string) *SQLBulder {
	builder.action = SQLActionSelect
	builder.fields = append(builder.fields, fields...)
	return builder
}

func (builder *SQLBulder) OrderBy(field string, order OrderEnum) *SQLBulder {
	builder.orders = append(builder.orders, &Order{
		Field: field,
		Order: order,
	})
	return builder
}

func (builder *SQLBulder) OrderBys(fields []string, order OrderEnum) *SQLBulder {
	for _, field := range fields {
		builder.orders = append(builder.orders, &Order{
			Field: field,
			Order: order,
		})
	}
	return builder
}

func (builder *SQLBulder) GroupBy(fields ...string) *SQLBulder {
	builder.groups = append(builder.groups, fields...)
	return builder
}

func (builder *SQLBulder) Limit(limit int64) *SQLBulder {
	builder.limitSize = Limit(limit)
	return builder
}

func (builder *SQLBulder) Offset(offset int64) *SQLBulder {
	builder.offsetSize = Offset(offset)
	return builder
}

func (builder *SQLBulder) Update(values map[string]interface{}) *SQLBulder {
	builder.action = SQLActionUpdate
	builder.values = []map[string]interface{}{values}
	return builder
}

func (builder *SQLBulder) ForceIndex(index string) *SQLBulder {
	builder.forceIndexName = ForceIndex(index)
	return builder
}

func (builder *SQLBulder) OnDuplicateUpdateKeys(keys ...string) *SQLBulder {
	builder.insert.onDupKeyUpdates = keys
	return builder
}

func (builder *SQLBulder) Insert(values map[string]interface{}) *SQLBulder {
	builder.action = SQLActionInsert
	builder.values = []map[string]interface{}{values}
	return builder
}

func (builder *SQLBulder) InsertIgnore(values map[string]interface{}) *SQLBulder {
	builder.Insert(values)
	builder.insert.ignore = true
	return builder
}

func (builder *SQLBulder) BatchInsert(values []map[string]interface{}) *SQLBulder {
	builder.action = SQLActionInsert
	builder.values = values
	return builder
}

func (builder *SQLBulder) BatchInsertIgnore(values []map[string]interface{}) *SQLBulder {
	builder.action = SQLActionInsert
	builder.values = values
	builder.insert.ignore = true
	return builder
}

func (builder *SQLBulder) Delete() *SQLBulder {
	builder.action = SQLActionDelete
	return builder
}

func (builder *SQLBulder) Query() *SQLBulder {
	builder.action = SQLActionSelect
	return builder
}

func (builder *SQLBulder) IsSingleAggregationSum() bool {
	return builder.isSingleAggregationFun("[count|sum]")
}

func (builder *SQLBulder) IsSingleAggregationAvg() bool {
	return builder.isSingleAggregationFun("avg")
}

func (builder *SQLBulder) IsSingleAggregationMax() bool {
	return builder.isSingleAggregationFun("max")
}

func (builder *SQLBulder) IsSingleAggregationMin() bool {
	return builder.isSingleAggregationFun("min")
}

func (builder *SQLBulder) isSingleAggregationFun(funName string) bool {
	exp, _ := regexp.Compile(`(?i)` + funName + `\((.*?)\)`)
	return len(builder.fields) == 1 && exp.MatchString(builder.fields[0])
}

func (builder *SQLBulder) Build() (string, []interface{}, error) {
	return builder.build()
}

func (builder *SQLBulder) BuildWithTable(name string) (string, []interface{}, error) {
	builder.tableName = name
	return builder.build()
}

func (builder *SQLBulder) build() (string, []interface{}, error) {
	defer func() {
		putBackBuilder(builder)
	}()
	if builder.tableName == "" {
		return "", nil, _sqlError("empty table name")
	}
	switch builder.action {
	case SQLActionInsert:
		return builder.buildInsert()
	case SQLActionUpdate:
		return builder.buildUpdate()
	case SQLActionSelect:
		return builder.buildSelect()
	case SQLActionDelete:
		return builder.buildDelete()
	}
	return "", nil, _sqlError("wrong action")
}

func (builder *SQLBulder) placeholder(typeKind reflect.Kind) string {
	if builder.parameterized {
		return "?"
	} else {
		if typeKind == reflect.String {
			return `"%s"`
		}
		return "%v"
	}
}

func (builder *SQLBulder) buildInsert() (string, []interface{}, error) {

	if len(builder.values) == 0 {
		return "", nil, _sqlError("wrong insert values")
	}

	fields := make([]string, 0, len(builder.values))
	fieldsHolderSb := new(strings.Builder)
	placeholders := make([]string, 0, len(builder.values))
	needEscapeFields := map[string]bool{}
	for field, val := range builder.values[0] {
		fields = append(fields, field)
		fieldsHolderSb.WriteString(_wrapField(field, builder.delimiter))
		fieldsHolderSb.WriteString(",")
		typeKind := reflect.TypeOf(val).Kind()
		placeholders = append(placeholders, builder.placeholder(typeKind))
		needEscapeFields[field] = !builder.parameterized && typeKind == reflect.String
	}
	fieldsHolder := fieldsHolderSb.String()
	fieldsHolder = _wrapBracket(fieldsHolder[:len(fieldsHolder)-1])
	batchHolder := _wrapBracket(strings.Join(placeholders, ","))
	values := builder.values
	args := make([]interface{}, 0, len(values)*len(fields))
	for i := range values {
		for _, field := range fields {
			arg := values[i][field]
			if needEscapeFields[field] {
				arg = _escape(arg.(string))
			}
			args = append(args, arg)
		}
	}
	ignore := ""
	if builder.insert.ignore {
		ignore = "IGNORE"
	}
	return _joinString([]string{
		"INSERT", ignore, "INTO", builder.tableName, fieldsHolder,
		"VALUES",
		_stringRepeatJoin(batchHolder, ",", len(values)),
		builder.insert.onDupKeyUpdates.String(),
	}, " "), args, nil
}

func (builder *SQLBulder) buildDelete() (string, []interface{}, error) {
	wheres, args := builder.whereList.string(false, builder.placeholder)
	if wheres == "" {
		return "", nil, _sqlError("can not delete without where conditions")
	}
	return _joinString([]string{"DELETE FROM", builder.tableName, _getWheres(wheres)}, " "), args, nil
}

func (builder *SQLBulder) buildUpdate() (string, []interface{}, error) {
	if len(builder.values) == 0 {
		return "", nil, _sqlError("wrong update values")
	}
	updatePartSb := new(strings.Builder)
	for field, val := range builder.values[0] {
		valtyp := reflect.TypeOf(val)
		if valtyp.String() == "mydb.UpdateRaw" {
			updatePartSb.WriteString(_wrapField(field, builder.delimiter))
			updatePartSb.WriteString(" = ")
			updatePartSb.WriteString(val.(UpdateRaw).Expr)
			updatePartSb.WriteString(",")
			builder.args = append(builder.args, val.(UpdateRaw).Args...)
		} else {
			typeKind := valtyp.Kind()
			updatePartSb.WriteString(_wrapField(field, builder.delimiter))
			updatePartSb.WriteString(" = ")
			updatePartSb.WriteString(builder.placeholder(typeKind))
			updatePartSb.WriteString(",")
			if !builder.parameterized && typeKind == reflect.String {
				val = _escape(val.(string))
			}
			builder.args = append(builder.args, val)
		}
	}
	wheres, args := builder.whereList.string(false, builder.placeholder)
	if wheres == "" {
		return "", nil, _sqlError("can not update without where conditions")
	}
	updatePart := updatePartSb.String()
	builder.args = append(builder.args, args...)
	return _joinString([]string{
		"UPDATE", builder.tableName, "SET", updatePart[:len(updatePart)-1],
		_getWheres(wheres)}, " "), builder.args, nil
}

func (builder *SQLBulder) buildSelect() (string, []interface{}, error) {
	wheres, args := builder.whereList.string(false, builder.placeholder)
	return _joinString([]string{
		"SELECT", strings.Join(builder.fields, ","), "FROM", builder.tableName,
		builder.forceIndexName.String(),
		_getWheres(wheres),
		builder.orders.String(),
		builder.groups.String(),
		builder.limitSize.String(),
		builder.offsetSize.String(),
	}, " "), args, nil
}

func _getWheres(wheres string) string {
	if wheres == "" {
		return ""
	}

	return "WHERE " + wheres
}

func _wrapField(field string, del string) string {
	return del + field + del
}

func _wrapBracket(field string) string {
	return "(" + field + ")"
}

func _sqlError(msg string) error {
	return fmt.Errorf("invalid sql: %s", msg)
}

func _escape(sql string) string {
	dest := make([]byte, 0, 2*len(sql))
	var escape byte

	for i := 0; i < len(sql); {
		r, w := utf8.DecodeRuneInString(sql[i:])

		escape = 0

		switch r {
		case 0: /* Must be escaped for 'mysql' */
			escape = '0'
		case '\n': /* Must be escaped for logs */
			escape = 'n'
		case '\r':
			escape = 'r'
		case '\\':
			escape = '\\'
		case '\'':
			escape = '\''
		case '"': /* Better safe than sorry */
			escape = '"'
		case '\032': /* This gives problems on Win32 */
			escape = 'Z'
		}

		if escape != 0 {
			dest = append(dest, '\\', escape)
		} else {
			dest = append(dest, sql[i:i+w]...)
		}

		i += w
	}

	return string(dest)
}

func _getInValues(vals []interface{}) (res []interface{}) {
	switch vals[0].(type) {
	case []int8:
		valFirst := vals[0].([]int8)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []int16:
		valFirst := vals[0].([]int16)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []int:
		valFirst := vals[0].([]int)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []int32:
		valFirst := vals[0].([]int32)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []int64:
		valFirst := vals[0].([]int64)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []uint8:
		valFirst := vals[0].([]uint8)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []uint16:
		valFirst := vals[0].([]uint16)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []uint:
		valFirst := vals[0].([]uint)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []uint32:
		valFirst := vals[0].([]uint32)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []uint64:
		valFirst := vals[0].([]uint64)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []string:
		valFirst := vals[0].([]string)
		if len(valFirst) == 0 {
			return nil
		}
		res = make([]interface{}, 0, len(valFirst))
		for _, val := range valFirst {
			res = append(res, val)
		}
	case []interface{}:
		vals := vals[0].([]interface{})
		if len(vals) == 0 {
			return nil
		}
		res = vals
	default:
		v := reflect.ValueOf(vals[0])
		if v.Kind() == reflect.Slice {
			l := v.Len()

			res = make([]interface{}, 0, l)
			for i := 0; i < l; i++ {
				res = append(res, v.Index(i).Interface())
			}
		} else {
			res = vals
		}
	}
	return
}

func _joinString(elems []string, sep string) string {
	switch len(elems) {
	case 0:
		return ""
	case 1:
		return elems[0]
	}
	n := len(sep) * (len(elems) - 1)
	for i := 0; i < len(elems); i++ {
		n += len(elems[i])
	}

	var b strings.Builder
	b.Grow(n)
	b.WriteString(elems[0])
	for _, s := range elems[1:] {
		// 增加逻辑，不会导致sep多插入一次
		if s == "" {
			continue
		}
		b.WriteString(sep)
		b.WriteString(s)
	}
	return b.String()
}

func _stringRepeatJoin(str, sep string, n int) string {
	if str == "" || n <= 1 {
		return str
	}
	return str + strings.Repeat(sep+str, n-1)
}
