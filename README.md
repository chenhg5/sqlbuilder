# Golang Simple SqlBuilder

example: 

```
package main

import (
    "fmt"
    "github.com/chenhg5/sqlbuilder"
)

func main() {
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
    fmt.Println("sql", sql, "args", args)
}
```