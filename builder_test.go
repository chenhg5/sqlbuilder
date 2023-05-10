package builder

import (
	"fmt"
	"testing"
)

func TestSQLBuild(t *testing.T) {
	sql, args, _ := New("test").
		Select("a", "b", "c", "max(d)").
		ForceIndex("_uk_a_b_c").
		Where("a", "=", 11).
		Wheres(func(w Wheres) Wheres {
			return w.Where("d", "=", 12).Where("e", ">=", "14").OrWhere("f", "<=", 15)
		}).
		OrWhere("c", "=", 13).
		OrWheres(func(w Wheres) Wheres {
			return w.Where("d", "=", 15).Where("e", "=", "147")
		}).
		WhereIn("g", []interface{}{1, 2, 3, 4, 5}).
		WhereIn("h", []interface{}{"jack"}).
		WhereCombineIn([]string{"aa", "bb"}, [][]interface{}{
			{10, 20},
			{11, 22},
		}).
		OrderBy("a", OrderAsc).
		OrderBy("b", OrderDesc).
		GroupBy("c").
		Limit(10).
		Offset(20).
		Query().
		BuildWithTable("test_p1")

	fmt.Println("select", sql, args)
	if sql != `SELECT a,b,c,max(d) FROM test_p1 FORCE INDEX(_uk_a_b_c) WHERE a = %v AND (d = %v AND e >= "%s" OR f <= %v) OR c = %v OR (d = %v AND e = "%s") AND g in (%v,%v,%v,%v,%v) AND h in ("%s") AND (aa,bb) in ((%v,%v),(%v,%v)) ORDER BY a ASC,b DESC GROUP BY c LIMIT 10 OFFSET 20` {
		t.Error("[select] wrong sql result")
	}

	sql, args, _ = New("test").Insert(map[string]interface{}{
		"a": 1,
		"b": 2,
	}).OnDuplicateUpdateKeys("a", "b").Build()

	fmt.Println("insert", sql, args)
	
	if sql != "INSERT INTO test (`a`,`b`) VALUES (%v,%v) ON DUPLICATE KEY UPDATE a = VALUES(a), b = VALUES(b)" {
		t.Error("[insert] wrong sql result")
	}

	sql, args, _ = New("test").BatchInsert([]map[string]interface{}{
		{
			"a": 1,
			"b": "jack",
			"c": true,
		}, {
			"a": 3,
			"b": "rose",
			"c": false,
		},
	}).Build()

	fmt.Println("batch insert", sql, args)

	sql, args, _ = New("test").Where("a", "=", 11).Update(map[string]interface{}{
		"c": 1,
		"b": 2,
		"d": "jack",
		"f": UpdateRaw{
			Expr: `%d | f`,
			Args: []interface{}{100},
		},
	}).Build()

	fmt.Println("update", sql, args)

	if sql != "UPDATE test SET `c` = %v,`b` = %v,`d` = \"%s\",`f` = %d | f WHERE a = %v" {
		t.Error("[update] wrong sql result")
	}

	sql, args, _ = New("test").Where("a", "=", 11).Delete().Build()

	fmt.Println("delete", sql, args)

	if sql != "DELETE FROM test WHERE a = %v" {
		t.Error("[delete] wrong sql result")
	}
}